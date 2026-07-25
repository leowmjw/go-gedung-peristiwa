package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardContainsBackends(t *testing.T) {
	s := NewServer(":8080")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	s.handleDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "MinIO") || !strings.Contains(body, "Tigris") {
		t.Fatalf("dashboard missing backend sections")
	}
}

func TestSimulateRequiresPost(t *testing.T) {
	s := NewServer(":8080")
	req := httptest.NewRequest(http.MethodGet, "/api/simulate/minio", nil)
	rec := httptest.NewRecorder()
	s.handleSimulateMinIO(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
