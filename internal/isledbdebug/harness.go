// Package isledbdebug runs focused IsleDB TailingReader experiments without GTFS or HTTP.
package isledbdebug

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/ankur-anand/isledb"
	"github.com/ankur-anand/isledb/blobstore"

	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const keyPrefix = "debug:tail:"

// Harness is a minimal single-prefix IsleDB writer + tailing reader setup.
type Harness struct {
	Backend  pipeline.Backend
	Prefix   string
	store    *blobstore.Store
	db       *isledb.DB
	writer   *isledb.Writer
	cacheDir string
}

// Open creates a fresh debug prefix (use unique suffix per run).
func Open(ctx context.Context, backend pipeline.Backend, prefixSuffix string) (*Harness, error) {
	cfg := pipeline.StoreConfigFromEnv(backend, prefixSuffix)
	cfg.CacheRoot = "tmp/cache/isledb-debug"
	agency := "debug-tail"

	store, err := pipeline.OpenAgencyStore(ctx, cfg, agency)
	if err != nil {
		return nil, err
	}
	db, err := isledb.OpenDB(ctx, store, isledb.DBOptions{})
	if err != nil {
		store.Close()
		return nil, err
	}
	wOpts := isledb.DefaultWriterOptions()
	wOpts.FlushInterval = 5 * time.Second
	writer, err := db.OpenWriter(ctx, wOpts)
	if err != nil {
		db.Close()
		store.Close()
		return nil, err
	}
	cache := pipeline.AgencyCacheDir(cfg, agency)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		writer.Close()
		db.Close()
		store.Close()
		return nil, err
	}
	return &Harness{
		Backend:  backend,
		Prefix:   agency + "-" + prefixSuffix,
		store:    store,
		db:       db,
		writer:   writer,
		cacheDir: cache,
	}, nil
}

func (h *Harness) key(seq int) []byte {
	return fmt.Appendf(nil, "%s%04d", keyPrefix, seq)
}

// WriteBatch writes sequential keys with distinct values.
func (h *Harness) WriteBatch(n int, startSeq int) error {
	for i := range n {
		seq := startSeq + i
		val := fmt.Appendf(nil, "v-%d-%d", time.Now().UnixNano(), seq)
		if err := h.writer.Put(h.key(seq), val); err != nil {
			return err
		}
	}
	return nil
}

func (h *Harness) Flush(ctx context.Context) error {
	return h.writer.Flush(ctx)
}

func (h *Harness) minKey() []byte { return []byte(keyPrefix) }
func (h *Harness) maxKey() []byte { return []byte(keyPrefix + "\xff") }

// CatchUpCount runs a one-shot tailing catch-up and returns keys seen.
func (h *Harness) CatchUpCount(ctx context.Context, startAfter []byte) (int, error) {
	opts := isledb.DefaultTailingReaderOpenOptions()
	opts.ReaderOptions.CacheDir = h.cacheDir
	tr, err := isledb.OpenTailingReader(ctx, h.store, opts)
	if err != nil {
		return 0, err
	}
	defer tr.Close()
	if err := tr.Start(); err != nil {
		return 0, err
	}
	co := isledb.CatchUpOptions{MinKey: h.minKey(), MaxKey: h.maxKey()}
	if len(startAfter) > 0 {
		co.StartAfterKey = startAfter
	}
	res, err := tr.CatchUp(ctx, co, func(kv isledb.KV) error { return nil })
	if err != nil {
		return 0, err
	}
	return res.Count, nil
}

// TailWatch counts keys delivered by Tail until ctx done. Returns total and max seq seen.
func (h *Harness) TailWatch(ctx context.Context, startAfter []byte) (<-chan int, <-chan error) {
	counts := make(chan int, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(counts)
		opts := isledb.DefaultTailingReaderOpenOptions()
		opts.ReaderOptions.CacheDir = h.cacheDir
		tr, err := isledb.OpenTailingReader(ctx, h.store, opts)
		if err != nil {
			errs <- err
			return
		}
		defer tr.Close()
		if err := tr.Start(); err != nil {
			errs <- err
			return
		}
		to := isledb.TailOptions{
			MinKey:       h.minKey(),
			MaxKey:       h.maxKey(),
			PollInterval: 100 * time.Millisecond,
		}
		if len(startAfter) > 0 {
			to.StartAfterKey = startAfter
		}
		err = tr.Tail(ctx, to, func(kv isledb.KV) error {
			select {
			case counts <- 1:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			errs <- err
		}
		close(errs)
	}()
	return counts, errs
}

// Close shuts down writer and store.
func (h *Harness) Close(ctx context.Context) error {
	if err := h.writer.Flush(ctx); err != nil {
		return err
	}
	if err := h.writer.Close(); err != nil {
		return err
	}
	if err := h.db.Close(); err != nil {
		return err
	}
	return h.store.Close()
}

// VisibilityRow is one flush-delay measurement.
type VisibilityRow struct {
	DelayMs  int
	KeysSeen int
	Err      string
}

// RunVisibility writes two batches separated by flush; measures when batch-2 appears via CatchUp.
// Isolates object-store visibility after flush (IsleDB vs S3 latency).
func RunVisibility(ctx context.Context, h *Harness, batchSize int) ([]VisibilityRow, error) {
	if err := h.WriteBatch(batchSize, 1); err != nil {
		return nil, err
	}
	if err := h.Flush(ctx); err != nil {
		return nil, err
	}
	checkpoint := h.key(batchSize)

	if err := h.WriteBatch(batchSize, batchSize+1); err != nil {
		return nil, err
	}
	flushAt := time.Now()
	if err := h.Flush(ctx); err != nil {
		return nil, err
	}

	delays := []int{0, 50, 100, 250, 500, 1000, 2000, 5000}
	var rows []VisibilityRow
	for _, d := range delays {
		elapsed := time.Since(flushAt)
		want := time.Duration(d)*time.Millisecond - elapsed
		if want > 0 {
			select {
			case <-ctx.Done():
				return rows, ctx.Err()
			case <-time.After(want):
			}
		}
		n, err := h.CatchUpCount(ctx, checkpoint)
		row := VisibilityRow{DelayMs: d, KeysSeen: n}
		if err != nil {
			row.Err = err.Error()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// IncrementalResult measures tail delivery of a post-checkpoint write batch.
type IncrementalResult struct {
	WroteKeys       int
	TailEvents      int
	FirstNewKeyMs   int64
	Timeout         bool
	ReplayBeforeNew int // reserved
}

// RunIncremental starts Tail after a checkpoint, writes a new batch, flush, waits for delivery.
func RunIncremental(ctx context.Context, h *Harness, priorKeys, newKeys int, wait time.Duration) (IncrementalResult, error) {
	checkpoint := h.key(priorKeys)
	tailCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	counts, errs := h.TailWatch(tailCtx, checkpoint)
	var total atomic.Int32
	firstNew := make(chan time.Time, 1)
	allReceived := make(chan struct{})
	gotFirst := atomic.Bool{}
	gotAll := atomic.Bool{}

	drain := make(chan struct{})
	go func() {
		defer close(drain)
		for range counts {
			n := total.Add(1)
			if gotFirst.CompareAndSwap(false, true) {
				firstNew <- time.Now()
			}
			if n >= int32(newKeys) && gotAll.CompareAndSwap(false, true) {
				close(allReceived)
			}
		}
	}()

	// Brief settle so tail attaches.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	if err := h.WriteBatch(newKeys, priorKeys+1); err != nil {
		return IncrementalResult{}, err
	}
	if err := h.Flush(ctx); err != nil {
		return IncrementalResult{}, err
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	var firstMs int64
	select {
	case ts := <-firstNew:
		firstMs = ts.Sub(start).Milliseconds()
	case <-timer.C:
		cancel()
		<-drain
		return IncrementalResult{WroteKeys: newKeys, TailEvents: int(total.Load()), Timeout: true}, nil
	case err := <-errs:
		if err != nil {
			return IncrementalResult{}, err
		}
	}

	remaining := wait - time.Since(start)
	if remaining <= 0 {
		cancel()
		<-drain
		return IncrementalResult{
			WroteKeys:     newKeys,
			TailEvents:    int(total.Load()),
			FirstNewKeyMs: firstMs,
			Timeout:       true,
		}, nil
	}
	deadline := time.NewTimer(remaining)
	defer deadline.Stop()
	select {
	case <-allReceived:
	case <-deadline.C:
		cancel()
		<-drain
		return IncrementalResult{
			WroteKeys:     newKeys,
			TailEvents:    int(total.Load()),
			FirstNewKeyMs: firstMs,
			Timeout:       true,
		}, nil
	case err := <-errs:
		if err != nil {
			return IncrementalResult{}, err
		}
	}
	cancel()
	<-drain
	return IncrementalResult{
		WroteKeys:     newKeys,
		TailEvents:    int(total.Load()),
		FirstNewKeyMs: firstMs,
	}, nil
}

// ReplayResult counts tail events without new writes (historical replay storm).
type ReplayResult struct {
	EventsInWindow int
	WindowMs       int
}

// RunReplay opens Tail with no StartAfterKey and counts events in window (no new writes).
func RunReplay(ctx context.Context, h *Harness, window time.Duration) (ReplayResult, error) {
	tailCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	counts, errs := h.TailWatch(tailCtx, nil)
	n := 0
	for range counts {
		n++
	}
	select {
	case err := <-errs:
		if err != nil {
			return ReplayResult{}, err
		}
	default:
	}
	return ReplayResult{EventsInWindow: n, WindowMs: int(window.Milliseconds())}, nil
}
