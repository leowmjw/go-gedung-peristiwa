package eventgen

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/leow/go-gedung-peristiwa/internal/model"
)

// Emission is one write to the pipeline (first emit or duplicate retry).
type Emission struct {
	Event  model.Event
	Retry  bool
	Unique bool // true for first emission of a logical event
}

// Config controls simulated traffic.
type Config struct {
	TotalUnique   int
	Duration      time.Duration
	DuplicateRate float64 // fraction of unique events also written twice
	NoDelay       bool    // skip wall-clock pacing (tests)
	Now           func() time.Time
	Rand          *rand.Rand
}

func DefaultConfig() Config {
	return Config{
		TotalUnique:   1000,
		Duration:      60 * time.Second,
		DuplicateRate: 0.10,
		Now:           time.Now,
		Rand:          rand.New(rand.NewSource(42)),
	}
}

var tenants = []struct {
	id    string
	share float64
}{
	{"tenant-alpha", 0.40},
	{"tenant-beta", 0.25},
	{"tenant-gamma", 0.15},
	{"tenant-delta", 0.12},
	{"tenant-epsilon", 0.08},
}

var eventTypes = []struct {
	t     model.EventType
	share float64
}{
	{model.EventTransaction, 0.50},
	{model.EventBalanceCheck, 0.25},
	{model.EventSettlement, 0.15},
	{model.EventKYCUpdate, 0.05},
	{model.EventFraudAlert, 0.05},
}

// Run generates emissions until TotalUnique unique events are produced over Duration.
func Run(ctx context.Context, cfg Config) ([]Emission, error) {
	if cfg.TotalUnique <= 0 {
		return nil, nil
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewSource(42))
	}

	start := cfg.Now()
	deadline := start.Add(cfg.Duration)
	interval := cfg.Duration / time.Duration(cfg.TotalUnique)
	if interval <= 0 {
		interval = time.Millisecond
	}

	var out []Emission
	unique := 0

	for unique < cfg.TotalUnique {
		now := cfg.Now()
		if now.After(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}

		elapsed := now.Sub(start)
		rateMul := trafficMultiplier(elapsed, cfg.Rand)
		wait := time.Duration(float64(interval) / rateMul)
		if !cfg.NoDelay && wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return out, ctx.Err()
			case <-timer.C:
			}
		}

		tenant := pickTenant(cfg.Rand)
		et := pickEventType(cfg.Rand)
		idKey, err := uuid.NewV7()
		if err != nil {
			return out, err
		}

		ev := model.Event{
			TenantID:       tenant,
			EventType:      et,
			Timestamp:      cfg.Now(),
			IdempotencyKey: idKey.String(),
			Payload:        randomPayload(cfg.Rand, et),
		}
		out = append(out, Emission{Event: ev, Retry: false, Unique: true})
		unique++

		if cfg.Rand.Float64() < cfg.DuplicateRate {
			retry := ev
			retry.Timestamp = cfg.Now()
			retry.Retry = true
			retry.Payload.Description = "retry"
			out = append(out, Emission{Event: retry, Retry: true, Unique: false})
		}
	}

	return out, nil
}

func trafficMultiplier(elapsed time.Duration, r *rand.Rand) float64 {
	sec := elapsed.Seconds()
	base := 1.0 + 0.5*math.Sin(2*math.Pi*sec/15.0)

	// Plateau windows (~1.5x for 5-10s)
	if int(sec)%20 >= 5 && int(sec)%20 < 12 {
		base *= 1.5
	}

	// Random spikes
	if r.Float64() < 0.05 {
		base *= 2 + r.Float64()*3
	}

	tenantScale := 0.5 + r.Float64()
	return max(base*tenantScale, 0.1)
}

func pickTenant(r *rand.Rand) string {
	x := r.Float64()
	var acc float64
	for _, t := range tenants {
		acc += t.share
		if x <= acc {
			return t.id
		}
	}
	return tenants[len(tenants)-1].id
}

func pickEventType(r *rand.Rand) model.EventType {
	x := r.Float64()
	var acc float64
	for _, et := range eventTypes {
		acc += et.share
		if x <= acc {
			return et.t
		}
	}
	return eventTypes[0].t
}

func randomPayload(r *rand.Rand, et model.EventType) model.Payload {
	p := model.Payload{
		AccountID: fmtAccount(r),
		Currency:  "MYR",
		Metadata: map[string]string{
			"source": "simulation",
		},
	}
	switch et {
	case model.EventTransaction, model.EventSettlement:
		p.Amount = 10 + r.Float64()*5000
		p.Description = "payment"
	case model.EventBalanceCheck:
		p.Description = "balance inquiry"
	case model.EventKYCUpdate:
		p.Description = "kyc refresh"
	case model.EventFraudAlert:
		p.Amount = r.Float64() * 100
		p.Description = "fraud signal"
	}
	return p
}

func fmtAccount(r *rand.Rand) string {
	return "acct-" + string(rune('A'+r.Intn(26))) + string(rune('0'+r.Intn(10)))
}

// ExpectedKeys returns unique event keys in first-emission order.
func ExpectedKeys(emissions []Emission) []string {
	var keys []string
	for _, e := range emissions {
		if e.Unique {
			keys = append(keys, e.Event.Key())
		}
	}
	return keys
}

// FastRun generates emissions without wall-clock delays (for tests).
func FastRun(ctx context.Context, cfg Config) ([]Emission, error) {
	var n int
	cfg.NoDelay = true
	cfg.Now = func() time.Time {
		t := time.Unix(0, int64(n)*int64(time.Millisecond))
		n++
		return t
	}
	return Run(ctx, cfg)
}
