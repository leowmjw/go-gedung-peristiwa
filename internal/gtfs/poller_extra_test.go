package gtfs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gtfsrt "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func TestBackoffDuration(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: 8 * time.Second}
	if got := b.duration(0); got != time.Second {
		t.Fatalf("attempt 0 = %v", got)
	}
	if got := b.duration(10); got != 8*time.Second {
		t.Fatalf("capped = %v", got)
	}
}

func TestParseFeedUsesEntityID(t *testing.T) {
	feed := Feed{Agency: "ktmb"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: new("entity-99"),
		Vehicle: &gtfsrt.VehiclePosition{
			Position: &gtfsrt.Position{Latitude: proto.Float32(3.5), Longitude: proto.Float32(101.0)},
		},
	}}))
	positions, skipped, err := ParseFeedBytes(feed, body)
	if err != nil || skipped != 0 || len(positions) != 1 {
		t.Fatalf("positions=%d skipped=%d err=%v", len(positions), skipped, err)
	}
	if positions[0].VehicleID != "entity-99" {
		t.Fatalf("vehicle id = %q", positions[0].VehicleID)
	}
}

func TestPollRateLimitRetry(t *testing.T) {
	feed := Feed{Agency: "ktmb", URL: "http://example/ratelimit"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: new("e1"),
		Vehicle: &gtfsrt.VehiclePosition{
			Vehicle:  &gtfsrt.VehicleDescriptor{Id: new("bus-1")},
			Position: &gtfsrt.Position{Latitude: proto.Float32(4.0), Longitude: proto.Float32(101.0)},
		},
	}}))

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	feed.URL = srv.URL

	p := &Poller{
		Client:  srv.Client(),
		Backoff: Backoff{Initial: 10 * time.Millisecond, Max: 20 * time.Millisecond},
	}
	positions, err := p.Poll(context.Background(), feed)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || calls < 2 {
		t.Fatalf("positions=%d calls=%d", len(positions), calls)
	}
}

func TestPollRateLimitRetryAfterHeader(t *testing.T) {
	feed := Feed{Agency: "ktmb"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: new("e1"),
		Vehicle: &gtfsrt.VehiclePosition{
			Vehicle:  &gtfsrt.VehicleDescriptor{Id: new("bus-1")},
			Position: &gtfsrt.Position{Latitude: proto.Float32(4.0), Longitude: proto.Float32(101.0)},
		},
	}}))

	var calls int
	var waited time.Duration
	last := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		now := time.Now()
		waited = now.Sub(last)
		last = now
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	feed.URL = srv.URL

	p := &Poller{
		Client:  srv.Client(),
		Backoff: Backoff{Initial: time.Second, Max: 20 * time.Millisecond},
	}
	positions, err := p.Poll(context.Background(), feed)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || calls != 2 {
		t.Fatalf("positions=%d calls=%d", len(positions), calls)
	}
	if waited > 200*time.Millisecond {
		t.Fatalf("Retry-After: 0 should not wait the exponential backoff (1s), waited %v", waited)
	}
}

func TestPollRateLimitGivesUpWithoutFinalSleep(t *testing.T) {
	feed := Feed{Agency: "ktmb"}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	feed.URL = srv.URL

	p := &Poller{
		Client:  srv.Client(),
		Backoff: Backoff{Initial: time.Millisecond, Max: 4 * time.Millisecond},
	}
	start := time.Now()
	_, err := p.Poll(context.Background(), feed)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != maxPollAttempts {
		t.Fatalf("calls = %d, want %d", calls, maxPollAttempts)
	}
	// Only maxPollAttempts-1 waits should occur; a wait after the last
	// attempt would be wasted since no retry follows.
	if elapsed > time.Duration(maxPollAttempts)*4*time.Millisecond {
		t.Fatalf("took too long, elapsed=%v", elapsed)
	}
}

func TestPollSequentialDoesNotOverlap(t *testing.T) {
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: new("e1"),
		Vehicle: &gtfsrt.VehiclePosition{
			Vehicle:  &gtfsrt.VehicleDescriptor{Id: new("bus-1")},
			Position: &gtfsrt.Position{Latitude: proto.Float32(4.0), Longitude: proto.Float32(101.0)},
		},
	}}))

	var inflight atomic.Int32
	var max atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for {
			cur := max.Load()
			if n <= cur || max.CompareAndSwap(cur, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inflight.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	p := &Poller{Client: srv.Client()}
	feeds := []Feed{
		{Agency: "a", URL: srv.URL + "/a"},
		{Agency: "b", URL: srv.URL + "/b"},
	}
	results := p.PollSequential(context.Background(), feeds)
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("results=%+v", results)
	}
	if max.Load() != 1 {
		t.Fatalf("overlapped in-flight requests: max=%d", max.Load())
	}
}
