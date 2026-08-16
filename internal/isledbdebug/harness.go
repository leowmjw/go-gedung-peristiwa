// Package isledbdebug runs focused IsleDB ChangeReader experiments without GTFS or HTTP.
package isledbdebug

import (
	"context"
	"fmt"
	"time"

	"github.com/ankur-anand/isledb"

	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const keyPrefix = "debug:feed:"

// Harness is a minimal single-prefix IsleDB writer + change reader setup.
type Harness struct {
	Backend pipeline.Backend
	Prefix  string
	*pipeline.PrefixDB
}

// Open creates a fresh debug prefix (use unique suffix per run).
func Open(ctx context.Context, backend pipeline.Backend, prefixSuffix string) (*Harness, error) {
	cfg := pipeline.StoreConfigFromEnv(backend, prefixSuffix)
	agency := "debug-feed"

	pdb, err := pipeline.OpenPrefixDB(ctx, pipeline.PrefixOpenConfig{
		Store:      cfg,
		PrefixID:   agency,
		FlushEvery: 5 * time.Second,
		Retention:  backend != pipeline.BackendMemory,
		RunMaint:   backend != pipeline.BackendMemory,
		MaintCtx:   ctx,
	})
	if err != nil {
		return nil, err
	}
	return &Harness{
		Backend: backend,
		Prefix:  agency + "-" + prefixSuffix,
		PrefixDB: pdb,
	}, nil
}

func (h *Harness) key(seq int) []byte {
	return fmt.Appendf(nil, "%s%04d", keyPrefix, seq)
}

// WriteBatch writes sequential keys with distinct values.
func (h *Harness) WriteBatch(ctx context.Context, n int, startSeq int) error {
	for i := range n {
		seq := startSeq + i
		val := fmt.Appendf(nil, "v-%d-%d", time.Now().UnixNano(), seq)
		if err := h.Writer.Put(ctx, h.key(seq), val); err != nil {
			return err
		}
	}
	return nil
}

func (h *Harness) Flush(ctx context.Context) error {
	if err := h.Writer.Flush(ctx); err != nil {
		return err
	}
	return pipeline.RefreshReader(ctx, h.Reader)
}

func (h *Harness) minKey() []byte { return []byte(keyPrefix) }
func (h *Harness) maxKey() []byte { return []byte(keyPrefix + "\xff") }

func (h *Harness) readChangesSince(ctx context.Context, cursor isledb.ChangeCursor) (int, isledb.ChangeCursor, error) {
	cr, err := h.DB.OpenChangeReader(ctx)
	if err != nil {
		return 0, cursor, err
	}
	defer cr.Close()

	if cursor.IsZero() {
		bounds, err := cr.Bounds(ctx)
		if err != nil {
			return 0, cursor, err
		}
		cursor = bounds.Head
	}

	opts := isledb.DefaultChangeReadOptions()
	total := 0
	next := cursor
	for {
		page, err := cr.Read(ctx, next, opts)
		if err != nil {
			return total, next, err
		}
		for _, ch := range page.Changes {
			key := string(ch.Key)
			if key >= string(h.minKey()) && key <= string(h.maxKey()) {
				total++
			}
		}
		next = page.Next
		if page.CaughtUp() {
			break
		}
	}
	return total, next, nil
}

func (h *Harness) drainToHead(ctx context.Context) (isledb.ChangeCursor, error) {
	cr, err := h.DB.OpenChangeReader(ctx)
	if err != nil {
		return isledb.ChangeCursor{}, err
	}
	defer cr.Close()

	bounds, err := cr.Bounds(ctx)
	if err != nil {
		return isledb.ChangeCursor{}, err
	}
	if bounds.Oldest.IsZero() {
		return bounds.Head, nil
	}

	opts := isledb.DefaultChangeReadOptions()
	cursor := bounds.Oldest
	for {
		page, err := cr.Read(ctx, cursor, opts)
		if err != nil {
			return cursor, err
		}
		cursor = page.Next
		if page.CaughtUp() {
			break
		}
	}
	return cursor, nil
}

// Close shuts down writer and store.
func (h *Harness) Close(ctx context.Context) error {
	return h.PrefixDB.Close(ctx)
}

// VisibilityRow is one flush-delay measurement.
type VisibilityRow struct {
	DelayMs  int
	KeysSeen int
	Err      string
}

// RunVisibility writes two batches separated by flush; measures when batch-2 appears via ChangeReader.
func RunVisibility(ctx context.Context, h *Harness, batchSize int) ([]VisibilityRow, error) {
	if err := h.WriteBatch(ctx, batchSize, 1); err != nil {
		return nil, err
	}
	if err := h.Flush(ctx); err != nil {
		return nil, err
	}
	head, err := h.drainToHead(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.WriteBatch(ctx, batchSize, batchSize+1); err != nil {
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
		n, _, err := h.readChangesSince(ctx, head)
		row := VisibilityRow{DelayMs: d, KeysSeen: n}
		if err != nil {
			row.Err = err.Error()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// IncrementalResult measures change-feed delivery of a post-checkpoint write batch.
type IncrementalResult struct {
	WroteKeys     int
	TailEvents    int
	FirstNewKeyMs int64
	Timeout       bool
}

// RunIncremental drains to head, writes a new batch, flush, polls change reader until delivery.
func RunIncremental(ctx context.Context, h *Harness, priorKeys, newKeys int, wait time.Duration) (IncrementalResult, error) {
	head, err := h.drainToHead(ctx)
	if err != nil {
		return IncrementalResult{}, err
	}

	start := time.Now()
	if err := h.WriteBatch(ctx, newKeys, priorKeys+1); err != nil {
		return IncrementalResult{}, err
	}
	if err := h.Flush(ctx); err != nil {
		return IncrementalResult{}, err
	}

	deadline := time.Now().Add(wait)
	var firstMs int64
	gotFirst := false
	total := 0
	for time.Now().Before(deadline) {
		n, _, err := h.readChangesSince(ctx, head)
		if err != nil {
			return IncrementalResult{}, err
		}
		if n > total {
			if !gotFirst {
				firstMs = time.Since(start).Milliseconds()
				gotFirst = true
			}
			total = n
		}
		if total >= newKeys {
			return IncrementalResult{
				WroteKeys:     newKeys,
				TailEvents:    total,
				FirstNewKeyMs: firstMs,
			}, nil
		}
		select {
		case <-ctx.Done():
			return IncrementalResult{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return IncrementalResult{
		WroteKeys:  newKeys,
		TailEvents: total,
		FirstNewKeyMs: firstMs,
		Timeout:    true,
	}, nil
}

// ReplayResult counts change-feed events without new writes.
type ReplayResult struct {
	EventsInWindow int
	WindowMs       int
}

// RunReplay reads historical changes from Oldest without new writes.
func RunReplay(ctx context.Context, h *Harness, window time.Duration) (ReplayResult, error) {
	n, err := pipeline.CountChangeFeed(ctx, h.DB)
	if err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{EventsInWindow: n, WindowMs: int(window.Milliseconds())}, nil
}
