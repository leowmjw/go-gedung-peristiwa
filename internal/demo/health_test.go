package demo_test

import (
	"errors"
	"testing"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
)

func TestWriteHealthDefaultThreshold(t *testing.T) {
	h := demo.NewWriteHealth(0)
	for i := 0; i < demo.DefaultWriteHealthThreshold-1; i++ {
		h.RecordFailure(errors.New("boom"))
		if !h.Healthy() {
			t.Fatalf("failure %d: expected still healthy below threshold %d", i+1, demo.DefaultWriteHealthThreshold)
		}
	}
	h.RecordFailure(errors.New("boom"))
	if h.Healthy() {
		t.Fatal("expected unhealthy once consecutive failures reach the default threshold")
	}
}

func TestWriteHealthCustomThreshold(t *testing.T) {
	h := demo.NewWriteHealth(1)
	if !h.Healthy() {
		t.Fatal("expected healthy before any failure")
	}
	h.RecordFailure(errors.New("boom"))
	if h.Healthy() {
		t.Fatal("expected unhealthy after one failure with threshold 1")
	}
}

func TestWriteHealthSuccessResetsFailures(t *testing.T) {
	h := demo.NewWriteHealth(2)
	h.RecordFailure(errors.New("boom"))
	h.RecordSuccess()
	h.RecordFailure(errors.New("boom again"))
	if !h.Healthy() {
		t.Fatal("expected healthy: success should have reset the streak before the second failure")
	}

	n, err := h.Status()
	if n != 1 || err == nil {
		t.Fatalf("Status() = (%d, %v), want (1, non-nil)", n, err)
	}
}

func TestWriteHealthStatusClearsOnSuccess(t *testing.T) {
	h := demo.NewWriteHealth(3)
	h.RecordFailure(errors.New("boom"))
	h.RecordFailure(errors.New("boom"))
	h.RecordSuccess()

	n, err := h.Status()
	if n != 0 || err != nil {
		t.Fatalf("Status() after success = (%d, %v), want (0, nil)", n, err)
	}
	if !h.Healthy() {
		t.Fatal("expected healthy after success clears the streak")
	}
}
