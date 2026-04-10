package caddy

import (
	"testing"
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
