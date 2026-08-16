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
