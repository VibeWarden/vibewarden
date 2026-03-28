package caddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
)

// fakeAuditEventLogger is a spy that captures audit events for handler tests.
type fakeAuditEventLogger struct {
	logged []audit.AuditEvent
}

func (f *fakeAuditEventLogger) Log(_ context.Context, ev audit.AuditEvent) error {
	f.logged = append(f.logged, ev)
	return nil
}

// hasAuditEventType returns true if the spy captured at least one event of the given type.
func (f *fakeAuditEventLogger) hasAuditEventType(eventType audit.EventType) bool {
	for _, ev := range f.logged {
		if ev.EventType == eventType {
			return true
		}
	}
	return false
}

// lastAuditEventOfType returns the last captured audit event matching the given type.
// Returns (zero, false) when no matching event exists.
func (f *fakeAuditEventLogger) lastAuditEventOfType(eventType audit.EventType) (audit.AuditEvent, bool) {
	for i := len(f.logged) - 1; i >= 0; i-- {
		if f.logged[i].EventType == eventType {
			return f.logged[i], true
		}
	}
	return audit.AuditEvent{}, false
}

// TestIPFilterHandler_EmitsAuditBlockedEvent verifies that a blocked request
// produces an audit.ip_filter.blocked event with correct actor and target fields.
func TestIPFilterHandler_EmitsAuditBlockedEvent(t *testing.T) {
	auditSpy := &fakeAuditEventLogger{}

	h := &IPFilterHandler{
		Config: IPFilterHandlerConfig{
			Mode:      "blocklist",
			Addresses: []string{"10.0.0.1"},
		},
		auditLogger: auditSpy,
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	// Override the audit logger set by Provision with our spy.
	h.auditLogger = auditSpy

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Fatalf("ServeHTTP() unexpected error: %v", err)
	}

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if !auditSpy.hasAuditEventType(audit.EventTypeIPFilterBlocked) {
		t.Error("expected audit.ip_filter.blocked event but none was logged")
	}
	ev, ok := auditSpy.lastAuditEventOfType(audit.EventTypeIPFilterBlocked)
	if !ok {
		t.Fatal("no audit.ip_filter.blocked event found")
	}
	if ev.Outcome != audit.OutcomeFailure {
		t.Errorf("outcome = %q, want %q", ev.Outcome, audit.OutcomeFailure)
	}
	if ev.Actor.IP != "10.0.0.1" {
		t.Errorf("actor.ip = %q, want %q", ev.Actor.IP, "10.0.0.1")
	}
	if ev.Target.Path != "/api" {
		t.Errorf("target.path = %q, want %q", ev.Target.Path, "/api")
	}
	if ev.Details["mode"] != "blocklist" {
		t.Errorf("details.mode = %v, want %q", ev.Details["mode"], "blocklist")
	}
}

// TestIPFilterHandler_NoAuditEventOnAllowedRequest verifies that an allowed
// request does not produce an audit event.
func TestIPFilterHandler_NoAuditEventOnAllowedRequest(t *testing.T) {
	auditSpy := &fakeAuditEventLogger{}

	h := &IPFilterHandler{
		Config: IPFilterHandlerConfig{
			Mode:      "blocklist",
			Addresses: []string{"10.0.0.1"},
		},
		auditLogger: auditSpy,
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	h.auditLogger = auditSpy

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "203.0.113.5:54321" // not in blocklist
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Fatalf("ServeHTTP() unexpected error: %v", err)
	}

	if len(auditSpy.logged) != 0 {
		t.Errorf("expected no audit events for allowed request, got %d", len(auditSpy.logged))
	}
}

// TestIPFilterHandler_NilAuditLoggerDoesNotPanic verifies that a nil
// auditLogger does not cause a panic when a request is blocked.
func TestIPFilterHandler_NilAuditLoggerDoesNotPanic(t *testing.T) {
	h := &IPFilterHandler{
		Config: IPFilterHandlerConfig{
			Mode:      "blocklist",
			Addresses: []string{"10.0.0.1"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	// Explicitly set to nil to test nil-safety.
	h.auditLogger = nil

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	w := httptest.NewRecorder()

	// Must not panic.
	_ = h.ServeHTTP(w, req, next)
}
