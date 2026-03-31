package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// maintenanceFakeEventLogger captures emitted events for test assertions.
type maintenanceFakeEventLogger struct {
	logged []events.Event
}

func (f *maintenanceFakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.logged = append(f.logged, ev)
	return nil
}

// ensure maintenanceFakeEventLogger satisfies the port interface at compile time.
var _ ports.EventLogger = (*maintenanceFakeEventLogger)(nil)

// nextHandler is a simple upstream handler that records whether it was called.
func nextHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestMaintenanceMiddleware_disabled(t *testing.T) {
	cfg := MaintenanceConfig{Enabled: false, Message: "irrelevant"}
	logger := &maintenanceFakeEventLogger{}
	mw := MaintenanceMiddleware(cfg, logger)

	called := false
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Error("next handler was not called when maintenance mode is disabled")
	}
	if len(logger.logged) != 0 {
		t.Errorf("no events should be logged when disabled, got %d", len(logger.logged))
	}
}

func TestMaintenanceMiddleware_enabled_blocks_regular_paths(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		method  string
		message string
	}{
		{
			name:    "blocks GET /",
			path:    "/",
			method:  http.MethodGet,
			message: "Service is under maintenance",
		},
		{
			name:    "blocks POST /api/checkout",
			path:    "/api/checkout",
			method:  http.MethodPost,
			message: "Scheduled maintenance until 03:00 UTC",
		},
		{
			name:    "blocks DELETE /api/users/42",
			path:    "/api/users/42",
			method:  http.MethodDelete,
			message: "Service is under maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := MaintenanceConfig{Enabled: true, Message: tt.message}
			logger := &maintenanceFakeEventLogger{}
			mw := MaintenanceMiddleware(cfg, logger)

			called := false
			handler := mw(nextHandler(&called))

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
			}
			if called {
				t.Error("next handler must not be called when maintenance mode blocks the request")
			}

			var body ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}
			if body.Error != "maintenance" {
				t.Errorf("body.Error = %q, want %q", body.Error, "maintenance")
			}
			if body.Message != tt.message {
				t.Errorf("body.Message = %q, want %q", body.Message, tt.message)
			}

			if len(logger.logged) != 1 {
				t.Fatalf("expected 1 event logged, got %d", len(logger.logged))
			}
			ev := logger.logged[0]
			if ev.EventType != events.EventTypeMaintenanceRequestBlocked {
				t.Errorf("event type = %q, want %q", ev.EventType, events.EventTypeMaintenanceRequestBlocked)
			}
			if ev.Payload["path"] != tt.path {
				t.Errorf("event payload path = %v, want %q", ev.Payload["path"], tt.path)
			}
			if ev.Payload["method"] != tt.method {
				t.Errorf("event payload method = %v, want %q", ev.Payload["method"], tt.method)
			}
		})
	}
}

func TestMaintenanceMiddleware_enabled_skips_vibewarden_paths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"health endpoint", "/_vibewarden/health"},
		{"ready endpoint", "/_vibewarden/ready"},
		{"metrics endpoint", "/_vibewarden/metrics"},
		{"nested path", "/_vibewarden/admin/plugins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := MaintenanceConfig{Enabled: true, Message: "down for maintenance"}
			logger := &maintenanceFakeEventLogger{}
			mw := MaintenanceMiddleware(cfg, logger)

			called := false
			handler := mw(nextHandler(&called))

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("path %q: status = %d, want %d", tt.path, rr.Code, http.StatusOK)
			}
			if !called {
				t.Errorf("path %q: next handler was not called for exempt path", tt.path)
			}
			if len(logger.logged) != 0 {
				t.Errorf("path %q: no events should be logged for exempt path, got %d", tt.path, len(logger.logged))
			}
		})
	}
}

func TestMaintenanceMiddleware_defaultMessage(t *testing.T) {
	cfg := MaintenanceConfig{Enabled: true, Message: ""}
	mw := MaintenanceMiddleware(cfg, nil)

	called := false
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Message != "Service is under maintenance" {
		t.Errorf("default message = %q, want %q", body.Message, "Service is under maintenance")
	}
}

func TestMaintenanceMiddleware_nilEventLoggerDoesNotPanic(t *testing.T) {
	cfg := MaintenanceConfig{Enabled: true, Message: "maintenance"}
	mw := MaintenanceMiddleware(cfg, nil) // nil logger must not panic

	called := false
	handler := mw(nextHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	// This must not panic.
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
