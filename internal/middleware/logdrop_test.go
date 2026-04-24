package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// errEventLogger is a ports.EventLogger fake that returns a configurable error.
type errEventLogger struct {
	err error
}

func (f *errEventLogger) Log(_ context.Context, _ events.Event) error {
	return f.err
}

// errAuditEventLogger is a ports.AuditEventLogger fake that returns a configurable error.
type errAuditEventLogger struct {
	err error
}

func (f *errAuditEventLogger) Log(_ context.Context, _ audit.AuditEvent) error {
	return f.err
}

// fakeDropCounter records calls to IncEventLogDrop.
type fakeDropCounter struct {
	calls []dropCall
}

type dropCall struct {
	middleware string
	reason     string
}

func (f *fakeDropCounter) IncEventLogDrop(middleware, reason string) {
	f.calls = append(f.calls, dropCall{middleware, reason})
}

// Compile-time checks.
var _ ports.EventLogger = (*errEventLogger)(nil)
var _ ports.AuditEventLogger = (*errAuditEventLogger)(nil)
var _ ports.EventLogDropCounter = (*fakeDropCounter)(nil)

// installTestLogger replaces the global slog default with one writing JSON to
// buf. The original logger is restored via t.Cleanup.
func installTestLogger(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(orig) })
}

// mustUnmarshalWarn parses the JSON from buf and returns the first "WARN" log
// entry found, or nil if none.
func mustUnmarshalWarn(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	dec := json.NewDecoder(buf)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("failed to decode log JSON: %v", err)
		}
		if m["level"] == "WARN" {
			return m
		}
	}
	return nil
}

// testEvent creates a minimal events.Event with the given EventType.
func testEvent(eventType string) events.Event {
	return events.Event{
		SchemaVersion: "v1",
		EventType:     eventType,
	}
}

// testAuditEvent creates a minimal audit.AuditEvent.
func testAuditEvent(t *testing.T) audit.AuditEvent {
	t.Helper()
	ev, err := audit.NewAuditEvent(
		audit.EventTypeAuthSuccess,
		audit.Actor{IP: "127.0.0.1"},
		audit.Target{Path: "/test"},
		audit.OutcomeSuccess,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("testAuditEvent: %v", err)
	}
	return ev
}

func TestLogEvent(t *testing.T) {
	tests := []struct {
		name            string
		loggerErr       error
		mc              *fakeDropCounter
		wantWarn        bool
		wantCounterCall bool
		wantReason      string
	}{
		{
			name:            "happy path — no error, no warn",
			loggerErr:       nil,
			mc:              &fakeDropCounter{},
			wantWarn:        false,
			wantCounterCall: false,
		},
		{
			name:            "generic error — warn + counter with reason other",
			loggerErr:       errors.New("boom"),
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "other",
		},
		{
			name:            "context.Canceled — warn + counter with reason canceled",
			loggerErr:       context.Canceled,
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "canceled",
		},
		{
			name:            "context.DeadlineExceeded — warn + counter with reason deadline_exceeded",
			loggerErr:       context.DeadlineExceeded,
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "deadline_exceeded",
		},
		{
			name:            "nil logger — silent no-op",
			loggerErr:       nil,
			mc:              &fakeDropCounter{},
			wantWarn:        false,
			wantCounterCall: false,
		},
		{
			name:            "nil mc — warn fires, counter skipped",
			loggerErr:       errors.New("boom"),
			mc:              nil,
			wantWarn:        true,
			wantCounterCall: false,
			wantReason:      "other",
		},
	}

	const mw = "test"
	const evType = "auth.success"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			installTestLogger(t, &buf)

			var logger ports.EventLogger
			if tt.name != "nil logger — silent no-op" {
				logger = &errEventLogger{err: tt.loggerErr}
			}

			var mc ports.EventLogDropCounter
			if tt.mc != nil {
				mc = tt.mc
			}

			logEvent(context.Background(), logger, mc, mw, testEvent(evType))

			entry := mustUnmarshalWarn(t, &buf)
			if tt.wantWarn && entry == nil {
				t.Fatal("expected slog WARN entry, got none")
			}
			if !tt.wantWarn && entry != nil {
				t.Fatalf("unexpected slog WARN entry: %v", entry)
			}
			if tt.wantWarn {
				if got := entry["middleware"]; got != mw {
					t.Errorf("middleware = %q, want %q", got, mw)
				}
				if got := entry["event_type"]; got != evType {
					t.Errorf("event_type = %q, want %q", got, evType)
				}
				if got := entry["reason"]; got != tt.wantReason {
					t.Errorf("reason = %q, want %q", got, tt.wantReason)
				}
			}

			if tt.mc != nil {
				if tt.wantCounterCall && len(tt.mc.calls) == 0 {
					t.Fatal("expected counter call, got none")
				}
				if !tt.wantCounterCall && len(tt.mc.calls) != 0 {
					t.Fatalf("unexpected counter calls: %v", tt.mc.calls)
				}
				if tt.wantCounterCall {
					got := tt.mc.calls[0]
					if got.middleware != mw {
						t.Errorf("counter middleware = %q, want %q", got.middleware, mw)
					}
					if got.reason != tt.wantReason {
						t.Errorf("counter reason = %q, want %q", got.reason, tt.wantReason)
					}
				}
			}
		})
	}
}

func TestLogAudit(t *testing.T) {
	tests := []struct {
		name            string
		loggerErr       error
		mc              *fakeDropCounter
		nilLogger       bool
		wantWarn        bool
		wantCounterCall bool
		wantReason      string
	}{
		{
			name:            "happy path — no error, no warn",
			loggerErr:       nil,
			mc:              &fakeDropCounter{},
			wantWarn:        false,
			wantCounterCall: false,
		},
		{
			name:            "generic error — warn + counter with reason other",
			loggerErr:       errors.New("boom"),
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "other",
		},
		{
			name:            "context.Canceled — warn + counter with reason canceled",
			loggerErr:       context.Canceled,
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "canceled",
		},
		{
			name:            "context.DeadlineExceeded — warn + counter with reason deadline_exceeded",
			loggerErr:       context.DeadlineExceeded,
			mc:              &fakeDropCounter{},
			wantWarn:        true,
			wantCounterCall: true,
			wantReason:      "deadline_exceeded",
		},
		{
			name:            "nil logger — silent no-op",
			nilLogger:       true,
			mc:              &fakeDropCounter{},
			wantWarn:        false,
			wantCounterCall: false,
		},
		{
			name:            "nil mc — warn fires, counter skipped",
			loggerErr:       errors.New("boom"),
			mc:              nil,
			wantWarn:        true,
			wantCounterCall: false,
			wantReason:      "other",
		},
	}

	const mw = "test"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			installTestLogger(t, &buf)

			var logger ports.AuditEventLogger
			if !tt.nilLogger {
				logger = &errAuditEventLogger{err: tt.loggerErr}
			}

			var mc ports.EventLogDropCounter
			if tt.mc != nil {
				mc = tt.mc
			}

			logAudit(context.Background(), logger, mc, mw, testAuditEvent(t))

			entry := mustUnmarshalWarn(t, &buf)
			if tt.wantWarn && entry == nil {
				t.Fatal("expected slog WARN entry, got none")
			}
			if !tt.wantWarn && entry != nil {
				t.Fatalf("unexpected slog WARN entry: %v", entry)
			}
			if tt.wantWarn {
				if got := entry["middleware"]; got != mw {
					t.Errorf("middleware = %q, want %q", got, mw)
				}
				if got := entry["event_type"]; got != string(audit.EventTypeAuthSuccess) {
					t.Errorf("event_type = %q, want %q", got, string(audit.EventTypeAuthSuccess))
				}
				if got := entry["reason"]; got != tt.wantReason {
					t.Errorf("reason = %q, want %q", got, tt.wantReason)
				}
			}

			if tt.mc != nil {
				if tt.wantCounterCall && len(tt.mc.calls) == 0 {
					t.Fatal("expected counter call, got none")
				}
				if !tt.wantCounterCall && len(tt.mc.calls) != 0 {
					t.Fatalf("unexpected counter calls: %v", tt.mc.calls)
				}
				if tt.wantCounterCall {
					got := tt.mc.calls[0]
					if got.middleware != mw {
						t.Errorf("counter middleware = %q, want %q", got.middleware, mw)
					}
					if got.reason != tt.wantReason {
						t.Errorf("counter reason = %q, want %q", got.reason, tt.wantReason)
					}
				}
			}
		})
	}
}

func TestErrorReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"context.Canceled", context.Canceled, "canceled"},
		{"context.DeadlineExceeded", context.DeadlineExceeded, "deadline_exceeded"},
		{"wrapped Canceled", errors.Join(context.Canceled, errors.New("wrap")), "canceled"},
		{"wrapped DeadlineExceeded", errors.Join(context.DeadlineExceeded, errors.New("wrap")), "deadline_exceeded"},
		{"generic error", errors.New("boom"), "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorReason(tt.err)
			if got != tt.want {
				t.Errorf("errorReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
