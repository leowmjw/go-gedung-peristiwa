package httpapi

import (
	"app/internal/shared"
	"encoding/json"
	"net/http"
)

// HTTPMetricRequest represents a batch of metrics sent via HTTP
type HTTPMetricRequest struct {
	Metrics []shared.MetricPoint `json:"metrics"`
}

// HTTPMetricResponse represents the response to a metric submission
type HTTPMetricResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// writeJSONResponse writes a JSON response with the given status code
func writeJSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSONResponse(w, status, HTTPMetricResponse{
		Success: false,
		Error:   message,
	})
}
