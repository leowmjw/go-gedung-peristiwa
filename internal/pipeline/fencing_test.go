package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

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

// TestFencedWriterNeverSelfRecovers covers the edge case a bad Kubernetes
// rollout + rollback exposes: a "bad" new pod can open a Writer (fencing the
// good old pod) before it fails its own health checks and gets aborted. With
// maxUnavailable: 0, the old pod's ReplicaSet was never scaled down while the
// new one was pending, so — confirmed via web search against Kubernetes'
// documented rollback behavior and Argo Rollouts' abort behavior — a rollback
// or abort just leaves that same old pod process running (or "reactivates"
// its already-unscaled ReplicaSet); it does not restart it. That old pod's
// writer is now permanently fenced with nothing left in the rollout to fix
// it, and no new writer is opening because the bad ReplicaSet gets scaled to
// zero.
//
// isledb has no answer to this by design: unlike a TTL leader lock (e.g.
// "steal the lock if it hasn't been renewed in 10s"), a fenced *Writer* has
// no expiry to wait out — `fenced` is a plain atomic.Bool in writer.go with
// no timestamp field, so it can never flip back regardless of how much time
// passes with no competing writer. This test proves that directly: it hammers
// the fenced writer for a full second (an eternity next to any writer's
// normal flush cadence) and every attempt fails identically throughout.
// Recovery only ever comes from a *new* OpenWriter call — see
// TestRollingDeployFencing for proof that a fresh writer always succeeds
// immediately, no wait required.
//
// The operational conclusion (see AGENTS.md "Rolling deploys / fencing"):
// since nothing in Kubernetes/Argo Rollouts' own rollback path will restart
// this specific pod, our own liveness probe must fail on a writer stuck this
// way so kubelet restarts the container and a fresh process reopens the
// writer — that is the only recovery path, and it has to be triggered by us.
func TestFencedWriterNeverSelfRecovers(t *testing.T) {
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
	if err := writer1.Put(ctx, []byte("before-fence"), []byte("v1")); err != nil {
		t.Fatalf("put before fence: %v", err)
	}
	if err := writer1.Flush(ctx); err != nil {
		t.Fatalf("flush before fence: %v", err)
	}

	// The bad rollout's pod: opens a writer (fencing writer1) and then, in
	// this scenario, never becomes healthy and is eventually torn down —
	// modeled here by simply never touching writer2/db2 again after this.
	db2, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
	if err != nil {
		t.Fatalf("open db2 (the bad rollout's pod): %v", err)
	}
	defer db2.Close()
	if _, err := db2.OpenWriter(ctx, isledb.DefaultWriterOptions()); err != nil {
		t.Fatalf("open writer2 (fences writer1): %v", err)
	}

	// The bad pod is gone (scaled to zero after abort/rollback) and nothing
	// ever opens a fresh writer on its behalf. writer1's process is still
	// alive — Kubernetes never restarted it — but every write it attempts
	// must keep failing identically, with no self-healing over time.
	deadline := time.Now().Add(time.Second)
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		putErr := writer1.Put(ctx, []byte("stale"), []byte("must-not-appear"))
		flushErr := writer1.Flush(ctx)
		if putErr == nil && flushErr == nil {
			t.Fatalf("attempt %d: writer1 unexpectedly recovered on its own "+
				"(put=%v flush=%v) — isledb fencing must not self-expire", attempts, putErr, flushErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if attempts < 2 {
		t.Fatalf("test didn't actually retry over time, got %d attempt(s)", attempts)
	}
	t.Logf("writer1 failed identically across %d attempts over 1s with no competing writer — "+
		"confirms no TTL/lease-style self-recovery", attempts)

	// The only real recovery path: a brand new OpenWriter call, exactly like
	// a Kubernetes liveness-probe-triggered container restart would produce.
	db3, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
	if err != nil {
		t.Fatalf("open db3 (a restarted pod's fresh process): %v", err)
	}
	defer db3.Close()
	writer3, err := db3.OpenWriter(ctx, isledb.DefaultWriterOptions())
	if err != nil {
		t.Fatalf("open writer3: %v", err)
	}
	if err := writer3.Put(ctx, []byte("recovered"), []byte("v3")); err != nil {
		t.Fatalf("put on writer3: %v", err)
	}
	if err := writer3.Flush(ctx); err != nil {
		t.Fatalf("flush on writer3: %v", err)
	}
}
