package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type EventType string

const (
	EventTransaction  EventType = "transaction"
	EventBalanceCheck EventType = "balance_check"
	EventKYCUpdate    EventType = "kyc_update"
	EventFraudAlert   EventType = "fraud_alert"
	EventSettlement   EventType = "settlement"
)

type Payload struct {
	Amount      float64           `json:"amount,omitempty"`
	Currency    string            `json:"currency,omitempty"`
	AccountID   string            `json:"account_id,omitempty"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Event is the JSON value stored in IsleDB.
type Event struct {
	TenantID       string    `json:"tenant_id"`
	EventType      EventType `json:"event_type"`
	Timestamp      time.Time `json:"timestamp"`
	Payload        Payload   `json:"payload"`
	IdempotencyKey string    `json:"idempotency_key"`
	Retry          bool      `json:"retry,omitempty"`
}

// Key returns the IsleDB KV key: {tenant}:{event_type}:{idempotency_key}.
func (e Event) Key() string {
	return fmt.Sprintf("%s:%s:%s", e.TenantID, e.EventType, e.IdempotencyKey)
}

func (e Event) KeyBytes() []byte {
	return []byte(e.Key())
}

func (e Event) ValueBytes() ([]byte, error) {
	return json.Marshal(e)
}

// ParseEvent decodes a stored JSON value.
func ParseEvent(data []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(data, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// TenantPrefix returns the scan prefix for all events of a tenant.
func TenantPrefix(tenantID string) []byte {
	return []byte(tenantID + ":")
}

// TenantUpperBound returns an exclusive upper bound for tenant scans.
func TenantUpperBound(tenantID string) []byte {
	return prefixUpperBound(tenantID + ":")
}

// TypePrefix returns scan prefix for tenant + event type.
func TypePrefix(tenantID string, eventType EventType) []byte {
	return fmt.Appendf(nil, "%s:%s:", tenantID, eventType)
}

// TypeUpperBound returns exclusive upper bound for tenant + type scans.
func TypeUpperBound(tenantID string, eventType EventType) []byte {
	return prefixUpperBound(fmt.Sprintf("%s:%s:", tenantID, eventType))
}

func prefixUpperBound(prefix string) []byte {
	out := []byte(prefix)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] < 0xff {
			out[i]++
			return out[:i+1]
		}
	}
	return append(out, 0x00)
}

// TenantFromKey extracts tenant id from a KV key.
func TenantFromKey(key string) string {
	if i := strings.IndexByte(key, ':'); i > 0 {
		return key[:i]
	}
	return key
}
