package gtfs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VehiclePosition is the JSON value stored in IsleDB for one GPS reading.
type VehiclePosition struct {
	Agency    string    `json:"agency"`
	VehicleID string    `json:"vehicle_id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Bearing   float64   `json:"bearing,omitempty"`
	Speed     float64   `json:"speed,omitempty"`
	Route     string    `json:"route,omitempty"`
	Trip      string    `json:"trip,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Key returns the IsleDB KV key: {agency}:{vehicle_id}:{timestamp_ns}.
func (v VehiclePosition) Key() string {
	return fmt.Sprintf("%s:%s:%d", v.Agency, v.VehicleID, v.Timestamp.UnixNano())
}

func (v VehiclePosition) KeyBytes() []byte {
	return []byte(v.Key())
}

func (v VehiclePosition) ValueBytes() ([]byte, error) {
	return json.Marshal(v)
}

// ParseVehiclePosition decodes a stored JSON value.
func ParseVehiclePosition(data []byte) (VehiclePosition, error) {
	var v VehiclePosition
	if err := json.Unmarshal(data, &v); err != nil {
		return VehiclePosition{}, err
	}
	return v, nil
}

// AgencyPrefix returns the scan prefix for all positions of an agency.
func AgencyPrefix(agency string) []byte {
	return []byte(agency + ":")
}

// AgencyUpperBound returns an exclusive upper bound for agency scans.
func AgencyUpperBound(agency string) []byte {
	return prefixUpperBound(agency + ":")
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

// VehicleIDFromKey extracts vehicle_id from a position key.
func VehicleIDFromKey(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// TimestampNSFromKey extracts the nanosecond timestamp suffix from a position key.
func TimestampNSFromKey(key string) int64 {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return 0
	}
	ns, _ := strconv.ParseInt(parts[2], 10, 64)
	return ns
}
