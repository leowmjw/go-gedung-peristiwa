package gtfs

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	gtfsrt "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func TestValidMalaysiaBounds(t *testing.T) {
	cases := []struct {
		lat, lng float64
		want     bool
	}{
		{3.139, 101.687, true}, // KL
		{0.5, 101.0, false},    // too far south
		{8.0, 101.0, false},    // too far north
		{3.0, 99.0, false},     // too far west
		{3.0, 120.0, false},    // too far east
	}
	for _, tc := range cases {
		if got := ValidMalaysiaBounds(tc.lat, tc.lng); got != tc.want {
			t.Errorf("ValidMalaysiaBounds(%v, %v) = %v, want %v", tc.lat, tc.lng, got, tc.want)
		}
	}
}

func TestAllFeedsCount(t *testing.T) {
	if got := len(AllFeeds()); got != 15 {
		t.Fatalf("feeds = %d, want 15", got)
	}
}

func TestKLFeedsCount(t *testing.T) {
	if got := len(KLFeeds()); got != 2 {
		t.Fatalf("KL feeds = %d, want 2", got)
	}
}

func TestVehiclePositionKey(t *testing.T) {
	ts := time.Unix(1700000000, 123456789).UTC()
	v := VehiclePosition{
		Agency:    "ktmb",
		VehicleID: "train-1",
		Timestamp: ts,
	}
	want := "ktmb:train-1:1700000000123456789"
	if got := v.Key(); got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func testFeedMessage(entities []*gtfsrt.FeedEntity) *gtfsrt.FeedMessage {
	return &gtfsrt.FeedMessage{
		Header: &gtfsrt.FeedHeader{GtfsRealtimeVersion: new("2.0")},
		Entity: entities,
	}
}

func TestParseFeedMessage(t *testing.T) {
	feed := Feed{Agency: "ktmb", Type: "rail"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{
		{
			Id: new("e1"),
			Vehicle: &gtfsrt.VehiclePosition{
				Vehicle: &gtfsrt.VehicleDescriptor{Id: new("train-42")},
				Position: &gtfsrt.Position{
					Latitude:  proto.Float32(3.14),
					Longitude: proto.Float32(101.69),
					Bearing:   proto.Float32(90),
					Speed:     proto.Float32(12.5),
				},
				Timestamp: proto.Uint64(1700000000),
				Trip: &gtfsrt.TripDescriptor{
					TripId:  new("trip-1"),
					RouteId: new("U32"),
				},
			},
		},
		{
			Id: new("e2"),
			Vehicle: &gtfsrt.VehiclePosition{
				Position: &gtfsrt.Position{
					Latitude:  proto.Float32(0.5),
					Longitude: proto.Float32(101.0),
				},
			},
		},
	}))

	positions, skipped, err := ParseFeedBytes(feed, body)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	p := positions[0]
	if p.VehicleID != "train-42" || p.Route != "U32" || p.Agency != "ktmb" {
		t.Fatalf("unexpected position: %+v", p)
	}
}

func TestPollerHTTPMock(t *testing.T) {
	feed := Feed{Agency: "ktmb", URL: "http://example/ktmb"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: new("e1"),
		Vehicle: &gtfsrt.VehiclePosition{
			Vehicle:  &gtfsrt.VehicleDescriptor{Id: new("bus-1")},
			Position: &gtfsrt.Position{Latitude: proto.Float32(4.0), Longitude: proto.Float32(101.0)},
		},
	}}))

	p := &Poller{
		Client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	positions, err := p.Poll(context.Background(), feed)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || positions[0].VehicleID != "bus-1" {
		t.Fatalf("unexpected: %+v", positions)
	}
}

func mustMarshalFeed(t *testing.T, msg *gtfsrt.FeedMessage) []byte {
	t.Helper()
	if msg.Header == nil {
		msg.Header = &gtfsrt.FeedHeader{GtfsRealtimeVersion: new("2.0")}
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
