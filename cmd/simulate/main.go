package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
	"github.com/leow/go-gedung-peristiwa/internal/simulation"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		backend      = flag.String("backend", "minio", "storage backend: memory, minio, tigris")
		duration     = flag.Duration("duration", 60*time.Second, "simulation duration")
		events       = flag.Int("events", 1000, "unique events to generate")
		prefixSuffix = flag.String("prefix-suffix", "", "optional suffix for IsleDB prefixes")
		fast         = flag.Bool("fast", false, "skip wall-clock pacing")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	opts := simulation.Options{
		Backend:      pipeline.Backend(*backend),
		Duration:     *duration,
		Events:       *events,
		PrefixSuffix: *prefixSuffix,
		Fast:         *fast,
	}

	logger.Info("starting simulation",
		"backend", opts.Backend,
		"duration", opts.Duration,
		"events", opts.Events,
		"prefix_suffix", opts.PrefixSuffix,
	)

	res := simulation.Run(ctx, opts)
	if !res.OK {
		logger.Error("simulation failed", "err", res.Error)
		return 1
	}

	logger.Info("verification ok",
		"puts", res.PutCount,
		"unique_expected", res.UniqueExpected,
		"pre_compact_keys", res.PreCompactKeys,
		"post_compact_keys", res.PostCompactKeys,
	)

	fmt.Println("✅ Simulation passed")
	return 0
}
