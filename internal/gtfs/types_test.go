package gtfs

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gtfsrt "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func TestAgencyIDs(t *testing.T) {
	feeds := AllFeeds()
	ids := AgencyIDs(feeds)
	if len(ids) != len(feeds) {
		t.Fatalf("ids len = %d", len(ids))
	}
	if ids[0] != "ktmb" {
		t.Fatalf("first id = %q", ids[0])
	}
}

func TestVehiclePositionRoundTrip(t *testing.T) {
	orig := VehiclePosition{
		Agency:    "ktmb",
		VehicleID: "v1",
		Lat:       3.1,
		Lng:       101.2,
		Bearing:   90,
		Speed:     10,
		Route:     "E1",
		Trip:      "t1",
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	b, err := orig.ValueBytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseVehiclePosition(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.VehicleID != orig.VehicleID || got.Route != orig.Route {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestKeyHelpers(t *testing.T) {
	key := "ktmb:train-9:1700000000123456789"
	if got := VehicleIDFromKey(key); got != "train-9" {
		t.Fatalf("VehicleIDFromKey = %q", got)
	}
	if got := TimestampNSFromKey(key); got != 1700000000123456789 {
		t.Fatalf("TimestampNSFromKey = %d", got)
	}
}

func TestAgencyPrefixBounds(t *testing.T) {
	min := string(AgencyPrefix("ktmb"))
	max := string(AgencyUpperBound("ktmb"))
	if min != "ktmb:" {
		t.Fatalf("min = %q", min)
	}
	if max <= min {
		t.Fatalf("max %q not greater than min %q", max, min)
	}
}

func TestPollAllPartialFailure(t *testing.T) {
	goodFeed := Feed{Agency: "ktmb", URL: "http://example/good"}
	badFeed := Feed{Agency: "mybas-ipoh", URL: "http://example/bad"}
	body := mustMarshalFeed(t, testFeedMessage([]*gtfsrt.FeedEntity{{
		Id: proto.String("e1"),
		Vehicle: &gtfsrt.VehiclePosition{
			Vehicle:  &gtfsrt.VehicleDescriptor{Id: proto.String("bus-1")},
			Position: &gtfsrt.Position{Latitude: proto.Float32(4.0), Longitude: proto.Float32(101.0)},
		},
	}}))

	p := &Poller{
		Client: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == badFeed.URL {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader("error")),
						Header:     make(http.Header),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	results := p.PollAll(context.Background(), []Feed{goodFeed, badFeed})
	if len(results) != 2 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].Err != nil || len(results[0].Positions) != 1 {
		t.Fatalf("good feed: err=%v positions=%d", results[0].Err, len(results[0].Positions))
	}
	if results[1].Err == nil {
		t.Fatal("expected bad feed error")
	}
}
