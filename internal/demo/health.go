package demo

import "sync"

// DefaultWriteHealthThreshold is how many consecutive Write/FlushAll
// failures WriteHealth tolerates before reporting unhealthy.
const DefaultWriteHealthThreshold = 3

// WriteHealth tracks whether this process's IsleDB writer still works, based
// on consecutive Write/FlushAll failures observed by the poll loop.
//
// It exists because isledb exposes no way to detect "I've been fenced" via
// any exported error (see AGENTS.md "Rolling deploys / fencing") — a fenced
// writer never self-recovers, and nothing in a Kubernetes rollback restarts
// the pod that holds it. So instead of trying to identify the cause, any
// sustained run of write failures — whatever the cause — is treated as
// evidence this pod's writer may be permanently stuck, and reported as
// unhealthy so a liveness probe can let Kubernetes restart the container. A
// fresh process reclaims the writer immediately on restart; see
// internal/pipeline/fencing_test.go.
type WriteHealth struct {
	mu                  sync.Mutex
	threshold           int
	consecutiveFailures int
	lastErr             error
}

// NewWriteHealth returns a tracker that reports unhealthy once consecutive
// write failures reach threshold. threshold <= 0 uses DefaultWriteHealthThreshold.
func NewWriteHealth(threshold int) *WriteHealth {
	if threshold <= 0 {
		threshold = DefaultWriteHealthThreshold
	}
	return &WriteHealth{threshold: threshold}
}

// RecordSuccess resets the consecutive-failure count after a successful write.
func (h *WriteHealth) RecordSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutiveFailures = 0
	h.lastErr = nil
}

// RecordFailure records a write/flush failure.
func (h *WriteHealth) RecordFailure(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutiveFailures++
	h.lastErr = err
}

// Healthy reports whether the consecutive-failure count is still below threshold.
func (h *WriteHealth) Healthy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.consecutiveFailures < h.threshold
}

// Status returns the current consecutive-failure count and the most recent
// error (nil once a success has cleared it).
func (h *WriteHealth) Status() (consecutiveFailures int, lastErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.consecutiveFailures, h.lastErr
}
