package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/ankur-anand/isledb"
	"gocloud.dev/blob/memblob"
)

// TestRollingDeployFencing reproduces a Kubernetes rolling-update overlap:
// two processes (writer1 = old pod, writer2 = new pod) open a Writer on the
// same bucket+prefix while both are alive. It answers two questions from
// AGENTS.md's "Rolling deploys / fencing" section directly:
//
//  1. Is the old writer "stopped permanently", or does the system self-heal
//     the moment any writer opens, no matter how delayed? -> self-heals:
//     writer2 works normally immediately, and a still-later writer3 (opened
//     after a further, wholly artificial delay) also works normally. Fencing
//     is ownership/generation-based via manifest commits, not a lease with a
//     TTL that could get stuck or need to "expire".
//  2. Can application code reliably detect "I was fenced" via any exported
//     isledb error so it can proactively self-terminate? -> no. Fence errors
//     are wrapped in the unexported manifest.ErrFenced and are explicitly
//     excluded from ever becoming the exported isledb.ErrWriterFailed
//     (writer.go: `terminalOnError && !isFenceError(err)` before recording
//     ErrWriterFailed). This is why cmd/demo/main.go must not gate an
//     early self-shutdown on errors.Is(err, isledb.ErrWriterFailed) —
//     it will not fire for the fencing case it was written for.
func TestRollingDeployFencing(t *testing.T) {
	ctx := context.Background()
	bkt := memblob.OpenBucket(nil)
	defer bkt.Close()

	opts := defaultDBOptions("shared-prefix")

	db1, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer db1.Close()

	writer1, err := db1.OpenWriter(ctx, isledb.DefaultWriterOptions())
	if err != nil {
		t.Fatalf("open writer1: %v", err)
	}

	// writer1 (the old pod) is healthy and writing before any overlap.
	if err := writer1.Put(ctx, []byte("before-fence"), []byte("v1")); err != nil {
		t.Fatalf("put before fence: %v", err)
	}
	if err := writer1.Flush(ctx); err != nil {
		t.Fatalf("flush before fence: %v", err)
	}

	// A second process opens its own DB handle on the SAME bucket+prefix —
	// the new pod's process starting up during a rolling deploy, well
	// before it necessarily passes any readiness probe.
	db2, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	writer2, err := db2.OpenWriter(ctx, isledb.DefaultWriterOptions())
	if err != nil {
		t.Fatalf("open writer2: %v", err)
	}

	// writer1 is now fenced. Its next mutation is rejected.
	if err := writer1.Put(ctx, []byte("stale"), []byte("must-not-appear")); err != nil {
		t.Fatalf("buffer stale write on writer1: %v", err)
	}
	flushErr := writer1.Flush(ctx)
	if flushErr == nil {
		t.Fatal("expected writer1.Flush to fail once fenced by writer2")
	}

	// The crux of this test: application code cannot tell "I was fenced"
	// apart from any other terminal writer failure via the exported API.
	if errors.Is(flushErr, isledb.ErrWriterFailed) {
		t.Fatal("fencing must NOT satisfy errors.Is(err, isledb.ErrWriterFailed) " +
			"(writer.go deliberately excludes fence errors from that sentinel) " +
			"— if this ever starts passing, cmd/demo/main.go's stopIfFenced check " +
			"can safely rely on it and this test (and that code) should be updated")
	}
	if errors.Is(flushErr, isledb.ErrWriterClosed) {
		t.Fatal("fencing must not satisfy errors.Is(err, isledb.ErrWriterClosed) either")
	}
	t.Logf("writer1 fenced-flush error (no exported sentinel matches it): %v", flushErr)

	// writer2 (the new pod) is fully functional immediately — no delay,
	// no warm-up, no separate "become active" step.
	if err := writer2.Put(ctx, []byte("after-fence"), []byte("v2")); err != nil {
		t.Fatalf("put on writer2 after fencing writer1: %v", err)
	}
	if err := writer2.Flush(ctx); err != nil {
		t.Fatalf("flush on writer2 after fencing writer1: %v", err)
	}

	// Simulate "the new process rolling out is delayed": writer2 itself
	// never becomes ready, and a THIRD process only opens much later — the
	// eventual retry/replacement. Fencing has no TTL to wait out, so this
	// works immediately regardless of how much wall-clock time passed.
	db3, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
	if err != nil {
		t.Fatalf("open db3 (the eventually-successful rollout): %v", err)
	}
	defer db3.Close()

	writer3, err := db3.OpenWriter(ctx, isledb.DefaultWriterOptions())
	if err != nil {
		t.Fatalf("open writer3: %v", err)
	}
	if err := writer3.Put(ctx, []byte("after-delay"), []byte("v3")); err != nil {
		t.Fatalf("put on writer3: %v", err)
	}
	if err := writer3.Flush(ctx); err != nil {
		t.Fatalf("flush on writer3: %v", err)
	}

	// writer2 is now fenced by writer3, proving there is nothing special
	// about "the second writer" — ownership always follows whoever opened
	// most recently, so a delayed/retried rollout is not a degraded case.
	if err := writer2.Put(ctx, []byte("also-stale"), []byte("must-not-appear")); err != nil {
		t.Fatalf("buffer stale write on writer2: %v", err)
	}
	if err := writer2.Flush(ctx); err == nil {
		t.Fatal("expected writer2.Flush to fail once fenced by writer3")
	}

	// Durable state reflects exactly the committed writers, in order, with
	// no corruption from the overlap: no in-flight write from a stale
	// writer was ever accepted after it lost the fence.
	reader, err := db3.OpenReader(ctx, isledb.DefaultReaderOpenOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer reader.Close()

	for _, want := range []struct {
		key, value string
	}{
		{"before-fence", "v1"},
		{"after-fence", "v2"},
		{"after-delay", "v3"},
	} {
		got, found, err := reader.Get(ctx, []byte(want.key))
		if err != nil {
			t.Fatalf("get %q: %v", want.key, err)
		}
		if !found || string(got) != want.value {
			t.Fatalf("get %q = (%q, %v), want (%q, true)", want.key, got, found, want.value)
		}
	}
	for _, unwanted := range []string{"stale", "also-stale"} {
		_, found, err := reader.Get(ctx, []byte(unwanted))
		if err != nil {
			t.Fatalf("get %q: %v", unwanted, err)
		}
		if found {
			t.Fatalf("key %q from a fenced writer must not be visible", unwanted)
		}
	}
}
