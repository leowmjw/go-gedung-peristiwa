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

func main() {
	os.Exit(run())
}

func run() int {
	var (
		backend   = flag.String("backend", "memory", "memory, minio, or tigris")
		experiment = flag.String("experiment", "all", "visibility, incremental, replay, or all")
		prefix    = flag.String("prefix-suffix", "", "unique prefix suffix (default: timestamp)")
		wait      = flag.Duration("wait", 10*time.Second, "incremental tail wait timeout")
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
		return 1
	}
	defer h.Close(ctx)

	fmt.Printf("=== isledb-debug backend=%s prefix=%s experiment=%s ===\n", b, h.Prefix, *experiment)

	exp := strings.ToLower(*experiment)
	var failed bool
	if exp == "all" || exp == "visibility" {
		failed = printVisibility(ctx, h, b) || failed
	}
	if exp == "all" || exp == "incremental" {
		failed = printIncremental(ctx, h, b, *wait) || failed
	}
	if exp == "all" || exp == "replay" {
		failed = printReplay(ctx, h, b) || failed
	}
	if failed {
		fmt.Println("\n❌ at least one experiment looks unhealthy for this backend")
		return 1
	}
	fmt.Println("\n✅ experiments completed — compare backends to isolate IsleDB vs object-store")
	return 0
}

func printVisibility(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend) bool {
	const batch = 5
	fmt.Printf("\n--- visibility (flush batch-2 → delay → CatchUp) [%s] ---\n", b)
	rows, err := isledbdebug.RunVisibility(ctx, h, batch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "visibility: %v\n", err)
		return true
	}
	fmt.Printf("%8s %10s %s\n", "delay_ms", "keys_seen", "note")
	var firstVisible int = -1
	for _, r := range rows {
		note := ""
		if r.Err != "" {
			note = "err: " + r.Err
		} else if r.KeysSeen >= batch && firstVisible < 0 {
			firstVisible = r.DelayMs
			note = "← first full visibility"
		}
		fmt.Printf("%8d %10d %s\n", r.DelayMs, r.KeysSeen, note)
	}
	if firstVisible < 0 {
		fmt.Println("WARN: no delay saw all new keys within 5s")
		return true
	}
	fmt.Printf("interpret: post-flush visibility ~%dms on %s (want keys_seen>=%d)\n", firstVisible, b, batch)
	return firstVisible > 500 && b == pipeline.BackendMemory
}

func printIncremental(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend, wait time.Duration) bool {
	fmt.Printf("\n--- incremental (Tail running → write batch → flush) [%s] ---\n", b)
	// Seed store with prior keys so Tail has history (like demo).
	if err := h.WriteBatch(10, 1); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		return true
	}
	if err := h.Flush(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "seed flush: %v\n", err)
		return true
	}

	res, err := isledbdebug.RunIncremental(ctx, h, 10, 5, wait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "incremental: %v\n", err)
		return true
	}
	fmt.Printf("wrote=%d tail_events=%d first_new_ms=%d replay_before_new=%d timeout=%v\n",
		res.WroteKeys, res.TailEvents, res.FirstNewKeyMs, res.ReplayBeforeNew, res.Timeout)
	if res.Timeout || res.TailEvents < res.WroteKeys {
		fmt.Println("WARN: tail did not deliver all new keys — same failure mode as demo SSE")
		return true
	}
	fmt.Printf("interpret: incremental tail OK on %s (first key in %dms)\n", b, res.FirstNewKeyMs)
	return false
}

func printReplay(ctx context.Context, h *isledbdebug.Harness, b pipeline.Backend) bool {
	fmt.Printf("\n--- replay (Tail with no new writes) [%s] ---\n", b)
	if err := h.WriteBatch(20, 1); err != nil {
		fmt.Fprintf(os.Stderr, "replay seed: %v\n", err)
		return true
	}
	if err := h.Flush(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "replay flush: %v\n", err)
		return true
	}
	res, err := isledbdebug.RunReplay(ctx, h, 2*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		return true
	}
	fmt.Printf("replay_events_in_%dms=%d\n", res.WindowMs, res.EventsInWindow)
	if res.EventsInWindow > 0 {
		fmt.Printf("interpret: tail replays %d historical keys on connect — explains SSE flood\n", res.EventsInWindow)
	}
	return false
}
