package isledbdebug_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/isledbdebug"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestVisibilityMemory(t *testing.T) {
	ctx := context.Background()
	h, err := isledbdebug.Open(ctx, pipeline.BackendMemory, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(ctx)

	rows, err := isledbdebug.RunVisibility(ctx, h, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, r := range rows {
		if r.KeysSeen >= 3 {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("visibility failed on memory: %+v", rows)
	}
}

func TestIncrementalMemory(t *testing.T) {
	ctx := context.Background()
	h, err := isledbdebug.Open(ctx, pipeline.BackendMemory, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(ctx)

	if err := h.WriteBatch(5, 1); err != nil {
		t.Fatal(err)
	}
	if err := h.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := isledbdebug.RunIncremental(ctx, h, 5, 3, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Timeout || res.TailEvents < 3 {
		t.Fatalf("incremental: %+v", res)
	}
}
