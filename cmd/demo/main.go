package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		addr         = flag.String("addr", envOr("DEMO_HTTP_ADDR", ":8081"), "HTTP listen address")
		pollInterval = flag.Duration("poll-interval", 30*time.Second, "GTFS poll interval")
		backend      = flag.String("backend", "minio", "storage backend: memory, minio")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	b := pipeline.Backend(*backend)
	if b != pipeline.BackendMemory && b != pipeline.BackendMinIO {
		slog.Error("demo supports memory and minio backends only", "backend", b)
		return 1
	}

	feeds := gtfs.KLFeeds()
	cfg := pipeline.StoreConfigFromEnv(b, "demo-kl")
	cfg.CacheRoot = "tmp/cache/demo-kl"

	pipe, err := demo.NewPipeline(ctx, cfg, feeds)
	if err != nil {
		slog.Error("pipeline init failed", "err", err)
		return 1
	}
	defer pipe.Close(context.Background())

	poller := gtfs.DefaultPoller()
	go pollLoop(ctx, *pollInterval, poller, feeds, pipe)

	srv := demoweb.NewServer(pipe, feeds)
	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
	}

	go func() {
		slog.Info("demo server listening", "addr", *addr, "backend", b, "feeds", len(feeds))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server failed", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	slog.Info("demo stopped")
	return 0
}

func pollLoop(ctx context.Context, interval time.Duration, poller *gtfs.Poller, feeds []gtfs.Feed, pipe *demo.Pipeline) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pollOnce := func() {
		results := poller.PollAll(ctx, feeds)
		var all []gtfs.VehiclePosition
		for _, res := range results {
			if res.Err != nil {
				slog.Warn("feed poll failed", "agency", res.Feed.Agency, "err", res.Err)
				continue
			}
			if res.Skipped > 0 {
				slog.Debug("skipped outliers", "agency", res.Feed.Agency, "count", res.Skipped)
			}
			all = append(all, res.Positions...)
			slog.Info("feed polled", "agency", res.Feed.Agency, "vehicles", len(res.Positions))
		}
		if len(all) == 0 {
			return
		}
		puts, err := pipe.Write(all)
		if err != nil {
			slog.Error("write failed", "err", err)
			return
		}
		if err := pipe.FlushAll(ctx); err != nil {
			slog.Error("flush failed", "err", err)
			return
		}
		pipe.SetLastPoll(time.Now())
		pipe.NotifyPoll()
		slog.Info("poll complete", "positions", puts)
	}

	pollOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollOnce()
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
