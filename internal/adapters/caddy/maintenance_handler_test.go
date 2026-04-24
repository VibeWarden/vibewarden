package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestMaintenanceHandler_CaddyModule(t *testing.T) {
	h := MaintenanceHandler{}
	info := h.CaddyModule()
	if info.ID != "http.handlers.vibewarden_maintenance" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_maintenance")
	}
	if info.New == nil {
		t.Error("CaddyModule().New is nil")
	}
	module := info.New()
	if _, ok := module.(*MaintenanceHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *MaintenanceHandler", module)
	}
}

// TestMaintenanceHandler_ProvisionWith verifies that ProvisionWith builds the
// handler correctly with an explicit services struct.
func TestMaintenanceHandler_ProvisionWith(t *testing.T) {
	h := &MaintenanceHandler{
		Config: MaintenanceHandlerConfig{Message: "down for maintenance"},
	}
	if err := h.ProvisionWith(RuntimeServices{}); err != nil {
		t.Fatalf("ProvisionWith() unexpected error: %v", err)
	}
	if h.inner == nil {
		t.Error("ProvisionWith() did not create inner middleware")
	}
}

// TestMaintenanceHandler_ServeHTTP_Returns503 verifies that the maintenance handler
// blocks requests with 503 when enabled.
func TestMaintenanceHandler_ServeHTTP_Returns503(t *testing.T) {
	h := &MaintenanceHandler{
		Config: MaintenanceHandlerConfig{Message: "maintenance"},
	}
	if err := h.ProvisionWith(RuntimeServices{}); err != nil {
		t.Fatalf("ProvisionWith() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/app/page", nil)
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Fatalf("ServeHTTP() unexpected error: %v", err)
	}

	if nextCalled {
		t.Error("expected maintenance to block the request, but next was called")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
