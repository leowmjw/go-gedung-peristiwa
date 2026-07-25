package httpapi

import (
	"app/internal/shared"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBatchMetrics(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		requestBody   any
		wantStatus    int
		wantSuccess   bool
		wantErrSubstr string
	}{
		{
			name:   "valid request",
			method: http.MethodPost,
			requestBody: HTTPMetricRequest{
				Metrics: []shared.MetricPoint{
					{
						Timestamp: 1234567890,
						Value:     123.45,
						IsNull:    false,
						Labels: []shared.Label{
							{Name: "host", Value: "test1"},
						},
					},
				},
			},
			wantStatus:  http.StatusOK,
			wantSuccess: true,
		},
		{
			name:          "wrong method",
			method:        http.MethodGet,
			requestBody:   HTTPMetricRequest{},
			wantStatus:    http.StatusMethodNotAllowed,
			wantSuccess:   false,
			wantErrSubstr: "method not allowed",
		},
		{
			name:   "empty metrics",
			method: http.MethodPost,
			requestBody: HTTPMetricRequest{
				Metrics: []shared.MetricPoint{},
			},
			wantStatus:    http.StatusBadRequest,
			wantSuccess:   false,
			wantErrSubstr: "no metrics provided",
		},
		{
			name:          "invalid json",
			method:        http.MethodPost,
			requestBody:   "invalid json",
			wantStatus:    http.StatusBadRequest,
			wantSuccess:   false,
			wantErrSubstr: "invalid request format",
		},
	}

	service := NewMetricsService()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			if str, ok := tt.requestBody.(string); ok {
				body.WriteString(str)
			} else {
				if err := json.NewEncoder(&body).Encode(tt.requestBody); err != nil {
					t.Fatalf("Failed to encode request body: %v", err)
				}
			}

			req := httptest.NewRequest(tt.method, "/metrics/v1/batch", &body)
			rec := httptest.NewRecorder()

			service.HandleBatchMetrics(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("HandleBatchMetrics() status = %v, want %v", rec.Code, tt.wantStatus)
			}

			var resp HTTPMetricResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if resp.Success != tt.wantSuccess {
				t.Errorf("HandleBatchMetrics() success = %v, want %v", resp.Success, tt.wantSuccess)
			}

			if tt.wantErrSubstr != "" && (resp.Error == "" || !bytes.Contains([]byte(resp.Error), []byte(tt.wantErrSubstr))) {
				t.Errorf("HandleBatchMetrics() error = %v, want substring %v", resp.Error, tt.wantErrSubstr)
			}
		})
	}
}
