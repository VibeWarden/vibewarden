package otel

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
)

// TestErrorHandler_DedupTableIsBounded asserts that a high-cardinality error
// stream — an exporter embedding a changing detail in every message — cannot
// grow the dedup table without limit.
func TestErrorHandler_DedupTableIsBounded(t *testing.T) {
	h := NewErrorHandler(slog.New(slog.NewJSONHandler(io.Discard, nil)))

	for i := 0; i < 100*maxTrackedMessages; i++ {
		h.Handle(fmt.Errorf("failed to upload metrics: attempt %d", i))
	}

	h.mu.Lock()
	tracked := len(h.seen)
	h.mu.Unlock()

	if tracked > maxTrackedMessages {
		t.Errorf("tracked messages = %d, want at most %d", tracked, maxTrackedMessages)
	}
}
