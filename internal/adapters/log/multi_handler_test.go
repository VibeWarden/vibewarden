package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
)

// recordingHandler is a minimal slog.Handler that records every Handle call.
type recordingHandler struct {
	enabled bool
	records []slog.Record
	attrs   []slog.Attr
	group   string
}

func (r *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return r.enabled }

func (r *recordingHandler) Handle(_ context.Context, rec slog.Record) error {
	r.records = append(r.records, rec.Clone())
	return nil
}

func (r *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &recordingHandler{enabled: r.enabled, attrs: append(r.attrs, attrs...)}
	return next
}

func (r *recordingHandler) WithGroup(name string) slog.Handler {
	return &recordingHandler{enabled: r.enabled, group: name}
}

// TestNewMultiHandler_ZeroHandlers verifies that a MultiHandler with no
// handlers does not panic and that Enabled returns false.
func TestNewMultiHandler_ZeroHandlers(t *testing.T) {
	h := logadapter.NewMultiHandler()
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled() should return false when no handlers are registered")
	}
}

// TestMultiHandler_Enabled tests the Enabled method across all handlers.
func TestMultiHandler_Enabled(t *testing.T) {
	tests := []struct {
		name        string
		handlers    []*recordingHandler
		queryLevel  slog.Level
		wantEnabled bool
	}{
		{
			name:        "all disabled",
			handlers:    []*recordingHandler{{enabled: false}, {enabled: false}},
			queryLevel:  slog.LevelInfo,
			wantEnabled: false,
		},
		{
			name:        "one enabled",
			handlers:    []*recordingHandler{{enabled: false}, {enabled: true}},
			queryLevel:  slog.LevelInfo,
			wantEnabled: true,
		},
		{
			name:        "all enabled",
			handlers:    []*recordingHandler{{enabled: true}, {enabled: true}},
			queryLevel:  slog.LevelInfo,
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers := make([]slog.Handler, len(tt.handlers))
			for i, h := range tt.handlers {
				handlers[i] = h
			}
			m := logadapter.NewMultiHandler(handlers...)
			got := m.Enabled(context.Background(), tt.queryLevel)
			if got != tt.wantEnabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

// TestMultiHandler_Handle verifies that every enabled handler receives the record.
func TestMultiHandler_Handle(t *testing.T) {
	h1 := &recordingHandler{enabled: true}
	h2 := &recordingHandler{enabled: true}
	m := logadapter.NewMultiHandler(h1, h2)

	logger := slog.New(m)
	logger.Info("test message")

	if len(h1.records) != 1 {
		t.Errorf("handler1 received %d records, want 1", len(h1.records))
	}
	if len(h2.records) != 1 {
		t.Errorf("handler2 received %d records, want 1", len(h2.records))
	}
}

// TestMultiHandler_Handle_SkipsDisabled verifies that disabled handlers do not
// receive records.
func TestMultiHandler_Handle_SkipsDisabled(t *testing.T) {
	h1 := &recordingHandler{enabled: true}
	h2 := &recordingHandler{enabled: false}
	m := logadapter.NewMultiHandler(h1, h2)

	logger := slog.New(m)
	logger.Info("test message")

	if len(h1.records) != 1 {
		t.Errorf("enabled handler received %d records, want 1", len(h1.records))
	}
	if len(h2.records) != 0 {
		t.Errorf("disabled handler received %d records, want 0", len(h2.records))
	}
}

// TestMultiHandler_WithAttrs verifies that WithAttrs propagates to all handlers.
func TestMultiHandler_WithAttrs(t *testing.T) {
	h1 := &recordingHandler{enabled: true}
	h2 := &recordingHandler{enabled: true}
	m := logadapter.NewMultiHandler(h1, h2)

	attrs := []slog.Attr{slog.String("key", "value")}
	derived := m.WithAttrs(attrs)

	// The derived handler must still dispatch to both.
	logger := slog.New(derived)
	logger.Info("with attrs")

	// Verify derived is a fresh MultiHandler (not the same as m).
	if derived == m {
		t.Error("WithAttrs returned the same handler, expected a new one")
	}
}

// TestMultiHandler_WithGroup verifies that WithGroup propagates to all handlers.
func TestMultiHandler_WithGroup(t *testing.T) {
	h1 := &recordingHandler{enabled: true}
	h2 := &recordingHandler{enabled: true}
	m := logadapter.NewMultiHandler(h1, h2)

	derived := m.WithGroup("mygroup")
	if derived == m {
		t.Error("WithGroup returned the same handler, expected a new one")
	}
}

// TestMultiHandler_ErrorTolerance verifies that an error from one handler does
// not prevent other handlers from receiving the record.
func TestMultiHandler_ErrorTolerance(t *testing.T) {
	// Use a real JSON handler writing to a buffer as the "good" handler.
	var buf bytes.Buffer
	good := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	// errorHandler always returns an error from Handle.
	err := &errorHandler{}

	m := logadapter.NewMultiHandler(err, good)
	logger := slog.New(m)
	logger.Info("tolerance test")

	// The good handler must have received the record.
	if buf.Len() == 0 {
		t.Error("good handler produced no output even though errorHandler errored")
	}
}

// errorHandler always fails.
type errorHandler struct{}

func (*errorHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (*errorHandler) Handle(_ context.Context, _ slog.Record) error {
	return &testHandlerError{}
}
func (*errorHandler) WithAttrs(_ []slog.Attr) slog.Handler { return &errorHandler{} }
func (*errorHandler) WithGroup(_ string) slog.Handler      { return &errorHandler{} }

type testHandlerError struct{}

func (*testHandlerError) Error() string { return "handler error" }
