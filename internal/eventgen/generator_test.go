package eventgen

import (
	"context"
	"math/rand"
	"sort"
	"testing"
	"time"
)

func TestRunProducesUniqueEvents(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TotalUnique = 50
	cfg.DuplicateRate = 0.2
	cfg.Rand = rand.New(rand.NewSource(1))

	emissions, err := FastRun(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	unique := 0
	for _, e := range emissions {
		if e.Unique {
			unique++
		}
	}
	if unique != cfg.TotalUnique {
		t.Fatalf("unique = %d, want %d", unique, cfg.TotalUnique)
	}
	if len(emissions) <= unique {
		t.Fatalf("expected duplicates, got %d total %d unique", len(emissions), unique)
	}
}

func TestExpectedKeysUnique(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TotalUnique = 20
	cfg.Rand = rand.New(rand.NewSource(2))

	emissions, err := FastRun(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys := ExpectedKeys(emissions)
	if len(keys) != cfg.TotalUnique {
		t.Fatalf("keys len = %d", len(keys))
	}
	seen := make(map[string]bool)
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	if len(sorted) != len(keys) {
		t.Fatal("sort changed length")
	}
}

func TestTrafficMultiplierPositive(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for sec := 0; sec < 60; sec++ {
		m := trafficMultiplier(time.Duration(sec)*time.Second, r)
		if m <= 0 {
			t.Fatalf("multiplier %v at sec %d", m, sec)
		}
	}
}
