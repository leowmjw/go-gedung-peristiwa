package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/VictoriaMetrics/easyproto"
)

// MetricsService handles HTTP metrics submissions
type MetricsService struct {
	marshalerPool sync.Pool
}

// NewMetricsService creates a new HTTP metrics service
func NewMetricsService() *MetricsService {
	return &MetricsService{
		marshalerPool: sync.Pool{
			New: func() interface{} {
				return &easyproto.Marshaler{}
			},
		},
	}
}

// HandleBatchMetrics handles batch metric submissions via HTTP
func (s *MetricsService) HandleBatchMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HTTPMetricRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request format")
		return
	}

	if len(req.Metrics) == 0 {
		writeJSONError(w, http.StatusBadRequest, "no metrics provided")
		return
	}

	// Process each metric
	for _, metric := range req.Metrics {
		// Validate timestamp
		if metric.Timestamp == 0 {
			metric.Timestamp = time.Now().UnixNano()
		}

		// Log metric (similar to gRPC server)
		log.Printf("Received HTTP metric: timestamp=%d, value=%f, isNull=%v",
			metric.Timestamp, metric.Value, metric.IsNull)
		
		for _, label := range metric.Labels {
			log.Printf("Label: %s=%s", label.Name, label.Value)
		}
		
		if metric.Properties != nil {
			log.Printf("Property count: %d", len(metric.Properties.Items))
		}
	}

	// Send success response
	writeJSONResponse(w, http.StatusOK, HTTPMetricResponse{
		Success: true,
	})
}
