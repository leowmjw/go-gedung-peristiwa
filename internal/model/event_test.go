package model

import (
	"testing"
	"time"
)

func TestEventKey(t *testing.T) {
	e := Event{
		TenantID:       "tenant-alpha",
		EventType:      EventTransaction,
		IdempotencyKey: "01932e40-7c6d-7e8f-9a0b-1c2d3e4f5a6b",
	}
	want := "tenant-alpha:transaction:01932e40-7c6d-7e8f-9a0b-1c2d3e4f5a6b"
	if got := e.Key(); got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func TestEventValueBytesRoundTrip(t *testing.T) {
	e := Event{
		TenantID:       "tenant-beta",
		EventType:      EventFraudAlert,
		Timestamp:      time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
		IdempotencyKey: "01932e40-8f9a-0b1c-2d3e-4f5a6b7c8d9e",
		Payload: Payload{
			Amount:   99.5,
			Currency: "MYR",
		},
	}
	data, err := e.ValueBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseEvent(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TenantID != e.TenantID || parsed.EventType != e.EventType {
		t.Fatalf("round trip mismatch: %+v", parsed)
	}
	if parsed.Payload.Amount != e.Payload.Amount {
		t.Fatalf("payload amount = %v", parsed.Payload.Amount)
	}
}

func TestTenantFromKey(t *testing.T) {
	if got := TenantFromKey("tenant-alpha:transaction:abc"); got != "tenant-alpha" {
		t.Fatalf("got %q", got)
	}
}

func TestPrefixBounds(t *testing.T) {
	min := TypePrefix("tenant-alpha", EventTransaction)
	max := TypeUpperBound("tenant-alpha", EventTransaction)
	if string(min) >= string(max) {
		t.Fatalf("min %q should be < max %q", min, max)
	}
	key := []byte("tenant-alpha:transaction:01932e40-7c6d-7e8f-9a0b-1c2d3e4f5a6b")
	if string(key) < string(min) || string(key) >= string(max) {
		t.Fatalf("key %q outside [%q, %q)", key, min, max)
	}
}
