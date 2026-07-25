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

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
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

	b := pipeline.Backend(*backend)
	cfg := pipeline.StoreConfigFromEnv(b, *prefixSuffix)

	logger.Info("starting simulation",
		"backend", b,
		"duration", duration,
		"events", *events,
		"prefix_suffix", *prefixSuffix,
	)

	genCfg := eventgen.DefaultConfig()
	genCfg.TotalUnique = *events
	genCfg.Duration = *duration
	genCfg.NoDelay = *fast

	var emissions []eventgen.Emission
	var err error
	if *fast {
		emissions, err = eventgen.FastRun(ctx, genCfg)
	} else {
		emissions, err = eventgen.Run(ctx, genCfg)
	}
	if err != nil {
		logger.Error("event generation failed", "err", err)
		return 1
	}
	logger.Info("events generated", "total_writes", len(emissions), "unique", len(eventgen.ExpectedKeys(emissions)))

	p, err := pipeline.New(ctx, cfg, pipeline.AllTenantIDs)
	if err != nil {
		logger.Error("pipeline init failed", "err", err)
		return 1
	}
	defer func() {
		if err := p.Close(context.Background()); err != nil {
			logger.Error("pipeline close", "err", err)
		}
	}()

	puts, err := p.WriteEmissions(emissions)
	if err != nil {
		logger.Error("write failed", "err", err)
		return 1
	}
	logger.Info("writes complete", "puts", puts)

	if err := p.FlushAll(ctx); err != nil {
		logger.Error("flush failed", "err", err)
		return 1
	}

	preKeys, err := p.CountKeys(ctx)
	if err != nil {
		logger.Error("pre-compact scan failed", "err", err)
		return 1
	}
	logger.Info("pre-compaction keys", "count", preKeys)

	if err := p.CompactAll(ctx); err != nil {
		logger.Error("compaction failed", "err", err)
		return 1
	}

	postKeys, err := p.CountKeys(ctx)
	if err != nil {
		logger.Error("post-compact scan failed", "err", err)
		return 1
	}
	logger.Info("post-compaction keys", "count", postKeys)

	result, err := p.Verify(ctx, emissions)
	if err != nil {
		logger.Error("verification failed", "err", err)
		return 1
	}

	logger.Info("verification ok",
		"puts", result.PutCount,
		"unique_expected", result.UniqueKeysExpected,
		"pre_compact_keys", preKeys,
		"post_compact_keys", postKeys,
	)

	fmt.Println("✅ Simulation passed")
	return 0
}
