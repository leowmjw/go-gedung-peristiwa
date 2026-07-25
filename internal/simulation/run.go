package simulation

import (
	"context"
	"fmt"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

// Options configures a simulation run.
type Options struct {
	Backend      pipeline.Backend
	Duration     time.Duration
	Events       int
	PrefixSuffix string
	Fast         bool
	CacheRoot    string
}

// DefaultOptions returns MVP defaults (60s, 1000 events).
func DefaultOptions(backend pipeline.Backend) Options {
	return Options{
		Backend:  backend,
		Duration: 60 * time.Second,
		Events:   1000,
		Fast:     false,
		CacheRoot: "tmp/cache",
	}
}

// DevOptions returns quicker defaults for the dev HTTP UI.
func DevOptions(backend pipeline.Backend) Options {
	o := DefaultOptions(backend)
	o.Events = 100
	o.Fast = true
	return o
}

// Result is the outcome of a simulation run.
type Result struct {
	Backend           pipeline.Backend
	StartedAt         time.Time
	FinishedAt        time.Time
	OK                bool
	Error             string
	PutCount          int
	UniqueExpected    int
	PreCompactKeys    int
	PostCompactKeys   int
	TailByTenant      map[string]int
}

// Run executes the full MVP pipeline for one backend.
func Run(ctx context.Context, opts Options) Result {
	start := time.Now()
	res := Result{
		Backend:     opts.Backend,
		StartedAt:   start,
		TailByTenant: make(map[string]int),
	}

	cfg := pipeline.StoreConfigFromEnv(opts.Backend, opts.PrefixSuffix)
	if opts.CacheRoot != "" {
		cfg.CacheRoot = opts.CacheRoot
	}

	genCfg := eventgen.DefaultConfig()
	genCfg.TotalUnique = opts.Events
	genCfg.Duration = opts.Duration
	genCfg.NoDelay = opts.Fast

	var emissions []eventgen.Emission
	var err error
	if opts.Fast {
		emissions, err = eventgen.FastRun(ctx, genCfg)
	} else {
		emissions, err = eventgen.Run(ctx, genCfg)
	}
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = err.Error()
		return res
	}

	p, err := pipeline.New(ctx, cfg, pipeline.AllTenantIDs)
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("pipeline init: %v", err)
		return res
	}
	defer p.Close(context.Background())

	puts, err := p.WriteEmissions(emissions)
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("write: %v", err)
		return res
	}
	res.PutCount = puts

	if err := p.FlushAll(ctx); err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("flush: %v", err)
		return res
	}

	pre, err := p.CountKeys(ctx)
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("pre-compact scan: %v", err)
		return res
	}
	res.PreCompactKeys = pre

	if err := p.CompactAll(ctx); err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("compact: %v", err)
		return res
	}

	post, err := p.CountKeys(ctx)
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = fmt.Sprintf("post-compact scan: %v", err)
		return res
	}
	res.PostCompactKeys = post

	verify, err := p.Verify(ctx, emissions)
	if err != nil {
		res.FinishedAt = time.Now()
		res.OK = false
		res.Error = err.Error()
		res.UniqueExpected = verify.UniqueKeysExpected
		return res
	}

	res.UniqueExpected = verify.UniqueKeysExpected
	res.TailByTenant = verify.TailCountByTenant
	res.FinishedAt = time.Now()
	res.OK = true
	return res
}
