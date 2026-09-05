package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ankur-anand/isledb"

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
		pollInterval = flag.Duration("poll-interval", demo.DefaultPollInterval, "fallback GTFS poll interval when no session has chosen one")
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

	allFeeds := gtfs.AllFeeds()
	cfg := pipeline.StoreConfigFromEnv(b, "demo-kl")

	pipe, err := demo.NewPipeline(ctx, cfg, allFeeds)
	if err != nil {
		slog.Error("pipeline init failed", "err", err)
		return 1
	}
	defer pipe.Close(context.Background())

	sessions, err := demo.OpenSessionStore(demo.DefaultSessionStorePath)
	if err != nil {
		slog.Error("session store", "err", err)
		return 1
	}
	coordinator := demo.NewPollCoordinator(sessions, *pollInterval)
	poller := gtfs.DefaultPoller()

	pollNow := make(chan string, 1)
	go pollLoop(ctx, cancel, demo.PollTickInterval, poller, coordinator, pipe, pollNow)

	srv := demoweb.NewServer(pipe, pipe, sessions, func(regionID string) {
		select {
		case pollNow <- regionID:
		default:
		}
	})
	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
	}

	go func() {
		slog.Info("demo server listening", "addr", *addr, "backend", b, "feeds", len(allFeeds))
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

func pollLoop(ctx context.Context, stop context.CancelFunc, interval time.Duration, poller *gtfs.Poller, coordinator *demo.PollCoordinator, pipe *demo.Pipeline, pollNow <-chan string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// stopIfWriterDead handles a writer failure that isledb has marked
	// definitively terminal (ErrWriterFailed/ErrWriterClosed) — no retry can
	// ever succeed again for this Writer, so keep polling GTFS and logging on
	// every tick would just waste effort until this process is eventually
	// killed. Trigger shutdown now instead.
	//
	// This does NOT cover the Kubernetes rolling-deploy fencing case (a newer
	// pod's process opening the same IsleDB prefix): isledb deliberately
	// excludes fence errors from ErrWriterFailed (writer.go:
	// `terminalOnError && !isFenceError(err)`), and the underlying
	// manifest.ErrFenced sentinel is unexported, so application code has no
	// reliable way to detect "I was fenced" specifically — see
	// internal/pipeline/fencing_test.go, which proves this against the real
	// dependency, and "Rolling deploys / fencing" in AGENTS.md for why we
	// don't try to fast-exit on that case (treating every write failure as
	// "fenced, exit now" would kill the pod on an ordinary transient MinIO
	// error too).
	stopIfWriterDead := func(err error) bool {
		if !errors.Is(err, isledb.ErrWriterFailed) && !errors.Is(err, isledb.ErrWriterClosed) {
			return false
		}
		slog.Warn("writer permanently failed, stopping", "err", err)
		stop()
		return true
	}

	runPoll := func(feeds []gtfs.Feed, regionIDs []string) {
		if len(feeds) == 0 {
			return
		}
		agencyIDs := gtfs.AgencyIDs(feeds)
		pipe.SetLastPolledAgencies(agencyIDs)

		results := poller.PollSequential(ctx, feeds)
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
		if ctx.Err() != nil {
			return
		}
		if len(all) > 0 {
			puts, err := pipe.Write(ctx, all)
			if err != nil {
				if stopIfWriterDead(err) {
					return
				}
				slog.Error("write failed", "err", err)
				return
			}
			if err := pipe.FlushAll(ctx); err != nil {
				if stopIfWriterDead(err) {
					return
				}
				slog.Error("flush failed", "err", err)
				return
			}
			slog.Info("poll complete", "positions", puts, "regions", regionIDs)
		} else {
			slog.Warn("poll produced no positions, backing off", "regions", regionIDs)
		}
		now := time.Now()
		pipe.SetLastPoll(now)
		coordinator.MarkPolled(regionIDs, now)
		if len(all) > 0 {
			pipe.NotifyPoll()
		}
	}

	scheduled := func() {
		feeds, regions, err := coordinator.FeedsForScheduledPoll(time.Now())
		if err != nil {
			slog.Error("plan scheduled poll", "err", err)
			return
		}
		runPoll(feeds, regions)
	}

	forRegion := func(regionID string) {
		feeds, regions, err := coordinator.FeedsForRegionSwitch(time.Now(), regionID)
		if err != nil {
			slog.Error("plan region poll", "err", err, "region", regionID)
			return
		}
		if len(feeds) == 0 {
			slog.Debug("region poll skipped, cache fresh", "region", regionID)
			return
		}
		runPoll(feeds, regions)
	}

	scheduled()
	for {
		select {
		case <-ctx.Done():
			return
		case regionID := <-pollNow:
			forRegion(regionID)
		case <-ticker.C:
			scheduled()
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
