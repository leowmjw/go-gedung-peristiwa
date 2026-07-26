package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/isledbdebug"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const (
	visibilityBatch       = 5
	memoryVisibilityMaxMs = 500
	s3VisibilityMaxMs     = 5000
	incrementalFirstNewMs = 2000
	replayWindow          = 2 * time.Second
)

type verdictStatus string

const (
	verdictPass verdictStatus = "PASS"
	verdictFail verdictStatus = "FAIL"
	verdictInfo verdictStatus = "INFO"
)

type verdict struct {
	name     string
	status   verdictStatus
	expected string
	actual   string
	hint     string // set only when user should act
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		backend    = flag.String("backend", "memory", "memory, minio, or tigris")
		experiment = flag.String("experiment", "all", "visibility, incremental, replay, or all")
		prefix     = flag.String("prefix-suffix", "", "unique prefix suffix (default: timestamp)")
		wait       = flag.Duration("wait", 10*time.Second, "incremental tail wait timeout")
	)
	flag.Parse()

	if *prefix == "" {
		*prefix = fmt.Sprintf("run-%d", time.Now().Unix())
	}

	b := pipeline.Backend(*backend)
	if b == pipeline.BackendTigris {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			fmt.Fprintln(os.Stderr, "tigris backend requires AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY in .env")
			return 1
		}
	}

	ctx := context.Background()
	h, err := isledbdebug.Open(ctx, b, *prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open harness: %v\n", err)
		fmt.Fprintln(os.Stderr, "→ next: check .env credentials, MinIO (`mise run minio-setup`), and tmp/cache/isledb-debug permissions")
		return 1
	}
	defer h.Close(ctx)

	fmt.Printf("=== isledb-debug backend=%s prefix=%s experiment=%s ===\n", b, h.Prefix, *experiment)

	exp := strings.ToLower(*experiment)
	var verdicts []verdict
	if exp == "all" || exp == "visibility" {
		verdicts = append(verdicts, runVisibility(ctx, h, b))
	}
	if exp == "all" || exp == "incremental" {
		verdicts = append(verdicts, runIncremental(ctx, h, b, *wait))
	}
	if exp == "all" || exp == "replay" {
		verdicts = append(verdicts, runReplay(ctx, h, b))
	}

	printSummary(b, verdicts)
	if anyFailed(verdicts) {
		return 1
	}
	return 0
}

func visibilityBudget(b pipeline.Backend) int {
	if b == pipeline.BackendMemory {
		return memoryVisibilityMaxMs
	}
	return s3VisibilityMaxMs
}

func runVisibility(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend) verdict {
	budget := visibilityBudget(b)
	v := verdict{
		name:     "visibility",
		expected: fmt.Sprintf("keys_seen>=%d within %dms after flush", visibilityBatch, budget),
	}

	fmt.Printf("\n--- visibility (flush batch-2 → delay → CatchUp) [%s] ---\n", b)
	fmt.Printf("what: write %d keys, flush, write %d more, flush, then CatchUp at increasing delays\n",
		visibilityBatch, visibilityBatch)

	rows, err := isledbdebug.RunVisibility(ctx, h, visibilityBatch)
	if err != nil {
		v.status = verdictFail
		v.actual = "error: " + err.Error()
		v.hint = "re-run single experiment: go run ./cmd/isledb-debug/ --backend " + string(b) + " --experiment visibility"
		fmt.Fprintf(os.Stderr, "visibility: %v\n", err)
		return v
	}

	fmt.Printf("%8s %10s %s\n", "delay_ms", "keys_seen", "status")
	firstVisible := -1
	maxSeen := 0
	for _, r := range rows {
		status := ""
		if r.Err != "" {
			status = "err: " + r.Err
		} else if r.KeysSeen > maxSeen {
			maxSeen = r.KeysSeen
		}
		if r.KeysSeen >= visibilityBatch && firstVisible < 0 && r.Err == "" {
			firstVisible = r.DelayMs
			status = "← first full visibility"
		} else if firstVisible >= 0 && r.KeysSeen >= visibilityBatch {
			status = "ok"
		} else if r.KeysSeen < visibilityBatch && r.Err == "" {
			status = fmt.Sprintf("partial (%d/%d)", r.KeysSeen, visibilityBatch)
		}
		fmt.Printf("%8d %10d %s\n", r.DelayMs, r.KeysSeen, status)
	}

	switch {
	case firstVisible < 0:
		v.status = verdictFail
		v.actual = fmt.Sprintf("never reached %d keys (max_seen=%d by 5000ms)", visibilityBatch, maxSeen)
		v.hint = "run `mise run dev:isledb-debug-compare` — if memory passes but " + string(b) + " fails, keep poll+broadcast UI; do not use per-key tail for live map"
	case firstVisible > budget:
		v.status = verdictFail
		v.actual = fmt.Sprintf("first full visibility at %dms (budget %dms)", firstVisible, budget)
		v.hint = "sweep tail PollInterval and reader Refresh in internal/isledbdebug/harness.go, then re-run --backend " + string(b)
	default:
		v.status = verdictPass
		v.actual = fmt.Sprintf("first full visibility at %dms (keys_seen=%d)", firstVisible, visibilityBatch)
	}
	return v
}

func runIncremental(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend, wait time.Duration) verdict {
	const priorKeys, newKeys = 10, 5
	v := verdict{
		name: "incremental",
		expected: fmt.Sprintf("tail delivers %d/%d new keys, timeout=false, first_new_ms<%d",
			newKeys, newKeys, incrementalFirstNewMs),
	}

	fmt.Printf("\n--- incremental (Tail running → write batch → flush) [%s] ---\n", b)
	fmt.Printf("what: seed %d keys, start Tail after checkpoint, write %d more, flush, wait for tail\n",
		priorKeys, newKeys)

	if err := h.WriteBatch(priorKeys, 1); err != nil {
		v.status = verdictFail
		v.actual = "seed write failed: " + err.Error()
		v.hint = "check tmp/cache/isledb-debug is writable, then re-run"
		return v
	}
	if err := h.Flush(ctx); err != nil {
		v.status = verdictFail
		v.actual = "seed flush failed: " + err.Error()
		v.hint = "check backend connectivity (MinIO: `mise run minio-setup`), then re-run"
		return v
	}

	res, err := isledbdebug.RunIncremental(ctx, h, priorKeys, newKeys, wait)
	if err != nil {
		v.status = verdictFail
		v.actual = "error: " + err.Error()
		v.hint = "run `mise run dev:isledb-debug-compare` — if all backends fail, file upstream IsleDB issue with this log"
		fmt.Fprintf(os.Stderr, "incremental: %v\n", err)
		return v
	}

	fmt.Printf("wrote=%d tail_events=%d first_new_ms=%d timeout=%v (wait budget=%s)\n",
		res.WroteKeys, res.TailEvents, res.FirstNewKeyMs, res.Timeout, wait)

	switch {
	case res.Timeout:
		v.status = verdictFail
		v.actual = fmt.Sprintf("timeout after %s (tail_events=%d/%d)", wait, res.TailEvents, newKeys)
		v.hint = "re-run with longer wait: go run ./cmd/isledb-debug/ --backend " + string(b) + " --experiment incremental --wait 30s; then run `mise run dev:isledb-debug-compare`"
	case res.TailEvents < res.WroteKeys:
		v.status = verdictFail
		v.actual = fmt.Sprintf("tail_events=%d/%d", res.TailEvents, res.WroteKeys)
		v.hint = "ensure MinIO is up (`mise run minio-setup`), increase --wait, and compare memory vs " + string(b)
	case res.FirstNewKeyMs >= incrementalFirstNewMs:
		v.status = verdictFail
		v.actual = fmt.Sprintf("slow first key at %dms (budget %dms)", res.FirstNewKeyMs, incrementalFirstNewMs)
		v.hint = "do not rely on tail for live UI — verify demo uses NotifyPoll/LatestPositions (internal/demo/pipeline.go)"
	default:
		v.status = verdictPass
		v.actual = fmt.Sprintf("tail_events=%d/%d, first_new_ms=%d", res.TailEvents, res.WroteKeys, res.FirstNewKeyMs)
	}
	return v
}

func runReplay(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend) verdict {
	const seedKeys = 20
	v := verdict{
		name:     "replay",
		expected: "Tail replays existing keys on connect (observational only)",
	}

	fmt.Printf("\n--- replay (Tail with no new writes) [%s] ---\n", b)
	fmt.Printf("what: write %d keys, flush, open Tail with no StartAfterKey, count events for %s\n",
		seedKeys, replayWindow)

	if err := h.WriteBatch(seedKeys, 1); err != nil {
		v.status = verdictFail
		v.actual = "seed write failed: " + err.Error()
		v.hint = "check tmp/cache/isledb-debug is writable, then re-run"
		return v
	}
	if err := h.Flush(ctx); err != nil {
		v.status = verdictFail
		v.actual = "seed flush failed: " + err.Error()
		v.hint = "check backend connectivity, then re-run"
		return v
	}

	res, err := isledbdebug.RunReplay(ctx, h, replayWindow)
	if err != nil {
		v.status = verdictFail
		v.actual = "error: " + err.Error()
		v.hint = "re-run: go run ./cmd/isledb-debug/ --backend " + string(b) + " --experiment replay"
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		return v
	}

	fmt.Printf("replay_events_in_%dms=%d\n", res.WindowMs, res.EventsInWindow)
	if res.EventsInWindow > 0 {
		v.status = verdictInfo
		v.actual = fmt.Sprintf("%d historical keys replayed in %dms", res.EventsInWindow, res.WindowMs)
		return v
	}

	v.status = verdictFail
	v.actual = "no replay events (unexpected for seeded store)"
	v.hint = "re-run with fresh prefix: --prefix-suffix debug-$(date +%s); if still zero, rm -rf tmp/cache/isledb-debug and retry"
	return v
}

func anyFailed(verdicts []verdict) bool {
	for _, v := range verdicts {
		if v.status == verdictFail {
			return true
		}
	}
	return false
}

func printSummary(b pipeline.Backend, verdicts []verdict) {
	fmt.Printf("\n=== summary [%s] ===\n", b)
	fmt.Printf("%-14s %-5s  %s\n", "experiment", "verdict", "actual")
	for _, v := range verdicts {
		fmt.Printf("%-14s %-5s  %s\n", v.name, v.status, v.actual)
	}

	failed := failedVerdicts(verdicts)
	if len(failed) == 0 {
		fmt.Printf("\n✅ PASS [%s]\n", b)
		return
	}

	fmt.Printf("\n❌ FAIL [%s]\n", b)
	for _, v := range failed {
		if v.hint != "" {
			fmt.Printf("  %s: %s\n", v.name, v.hint)
		}
	}
	printFailureGuide(b, failed)
}

func failedVerdicts(verdicts []verdict) []verdict {
	var out []verdict
	for _, v := range verdicts {
		if v.status == verdictFail {
			out = append(out, v)
		}
	}
	return out
}

func printFailureGuide(b pipeline.Backend, failed []verdict) {
	var vis, inc bool
	for _, v := range failed {
		switch v.name {
		case "visibility":
			vis = true
		case "incremental":
			inc = true
		}
	}

	fmt.Println()
	switch {
	case vis || inc:
		fmt.Println("diagnose:")
		if b == pipeline.BackendMemory {
			fmt.Println("  memory failed → likely IsleDB or harness bug (not object-store)")
			fmt.Println("  → open upstream issue with output from: go test ./internal/isledbdebug/... -v")
		} else {
			fmt.Println("  → run: mise run dev:isledb-debug-compare")
			fmt.Println("  memory PASS + " + string(b) + " FAIL → object-store visibility; keep poll+broadcast UI")
			fmt.Println("  all backends FAIL incremental → IsleDB TailingReader or harness bug")
		}
		if vis && inc {
			fmt.Println("  both visibility and incremental failed → fix visibility first (flush/read path)")
		}
	default:
		fmt.Println("diagnose: replay-only failure — follow the replay next step above")
	}
}
