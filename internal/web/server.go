package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
	"github.com/leow/go-gedung-peristiwa/internal/simulation"
)

//go:embed dashboard.html
var dashboardFS embed.FS

type cardData struct {
	Name    string
	Action  string
	BV      backendView
	Running bool
}

// Server is the dev HTTP control plane for MVP simulations.
type Server struct {
	addr       string
	mu         sync.Mutex
	running    bool
	lastMinIO  *simulation.Result
	lastTigris *simulation.Result
}

// NewServer creates a dev server bound to addr (e.g. ":8080").
func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/simulate/minio", s.handleSimulateMinIO)
	mux.HandleFunc("/api/simulate/tigris", s.handleSimulateTigris)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	slog.Info("dev server listening", "addr", ln.Addr().String())
	return http.Serve(ln, mux)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := s.buildView(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTmpl.Execute(w, data); err != nil {
		slog.Error("template", "err", err)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.buildView(r.Context()))
}

func (s *Server) handleSimulateMinIO(w http.ResponseWriter, r *http.Request) {
	s.runSimulation(w, r, pipeline.BackendMinIO)
}

func (s *Server) handleSimulateTigris(w http.ResponseWriter, r *http.Request) {
	s.runSimulation(w, r, pipeline.BackendTigris)
}

func (s *Server) runSimulation(w http.ResponseWriter, r *http.Request, backend pipeline.Backend) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		http.Error(w, "simulation already running", http.StatusConflict)
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		opts := simulation.DevOptions(backend)
		opts.PrefixSuffix = fmt.Sprintf("run-%d", time.Now().Unix())
		res := simulation.Run(ctx, opts)

		s.mu.Lock()
		if backend == pipeline.BackendMinIO {
			s.lastMinIO = &res
		} else {
			s.lastTigris = &res
		}
		s.mu.Unlock()

		if res.OK {
			slog.Info("simulation passed", "backend", backend, "puts", res.PutCount)
		} else {
			slog.Error("simulation failed", "backend", backend, "err", res.Error)
		}
	}()

	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type dashboardView struct {
	Running   bool
	MinIO     backendView
	Tigris    backendView
	UpdatedAt time.Time
}

type backendView struct {
	Status  pipeline.BackendStatus
	LastRun *simulation.Result
}

func (s *Server) buildView(ctx context.Context) dashboardView {
	minCfg := pipeline.StoreConfigFromEnv(pipeline.BackendMinIO, "")
	tigCfg := pipeline.StoreConfigFromEnv(pipeline.BackendTigris, "")

	s.mu.Lock()
	running := s.running
	lastMin := s.lastMinIO
	lastTig := s.lastTigris
	s.mu.Unlock()

	return dashboardView{
		Running: running,
		MinIO: backendView{
			Status:  pipeline.Probe(ctx, minCfg),
			LastRun: lastMin,
		},
		Tigris: backendView{
			Status:  pipeline.Probe(ctx, tigCfg),
			LastRun: lastTig,
		},
		UpdatedAt: time.Now(),
	}
}

var dashboardTmpl = template.Must(func() (*template.Template, error) {
	raw, err := dashboardFS.ReadFile("dashboard.html")
	if err != nil {
		return nil, err
	}
	return template.New("dashboard").Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("15:04:05")
		},
		"backendCard": func(name, action string, bv backendView, running bool) cardData {
			return cardData{Name: name, Action: action, BV: bv, Running: running}
		},
	}).Parse(string(raw))
}())
