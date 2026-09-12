package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func uiFlowBoolPtr(b bool) *bool { return &b }

// TestAuthUIConfig_FlowToggleAccessors pins the "unset means enabled"
// semantics of auth.ui.show_registration / auth.ui.show_recovery (#1512).
func TestAuthUIConfig_FlowToggleAccessors(t *testing.T) {
	tests := []struct {
		name             string
		ui               config.AuthUIConfig
		wantRegistration bool
		wantRecovery     bool
	}{
		{
			name:             "unset defaults to enabled",
			ui:               config.AuthUIConfig{},
			wantRegistration: true,
			wantRecovery:     true,
		},
		{
			name: "explicitly enabled",
			ui: config.AuthUIConfig{
				ShowRegistration: uiFlowBoolPtr(true),
				ShowRecovery:     uiFlowBoolPtr(true),
			},
			wantRegistration: true,
			wantRecovery:     true,
		},
		{
			name: "explicitly disabled",
			ui: config.AuthUIConfig{
				ShowRegistration: uiFlowBoolPtr(false),
				ShowRecovery:     uiFlowBoolPtr(false),
			},
			wantRegistration: false,
			wantRecovery:     false,
		},
		{
			name:             "registration disabled only",
			ui:               config.AuthUIConfig{ShowRegistration: uiFlowBoolPtr(false)},
			wantRegistration: false,
			wantRecovery:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ui.IsRegistrationEnabled(); got != tt.wantRegistration {
				t.Errorf("IsRegistrationEnabled() = %v, want %v", got, tt.wantRegistration)
			}
			if got := tt.ui.IsRecoveryEnabled(); got != tt.wantRecovery {
				t.Errorf("IsRecoveryEnabled() = %v, want %v", got, tt.wantRecovery)
			}
		})
	}
}

// TestLoadStrict_AuthUIFlowToggles verifies the new keys are accepted by the
// strict loader and decoded as an explicit false rather than "unset".
func TestLoadStrict_AuthUIFlowToggles(t *testing.T) {
	content := `
auth:
  mode: kratos
  ui:
    mode: built-in
    show_registration: false
    show_recovery: false
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp config file: %v", err)
	}

	cfg, err := config.LoadStrict(cfgFile, "")
	if err != nil {
		t.Fatalf("LoadStrict() unexpected error: %v", err)
	}

	if cfg.Auth.UI.ShowRegistration == nil {
		t.Fatal("auth.ui.show_registration was not decoded")
	}
	if cfg.Auth.UI.ShowRecovery == nil {
		t.Fatal("auth.ui.show_recovery was not decoded")
	}
	if cfg.Auth.UI.IsRegistrationEnabled() {
		t.Error("IsRegistrationEnabled() = true, want false")
	}
	if cfg.Auth.UI.IsRecoveryEnabled() {
		t.Error("IsRecoveryEnabled() = true, want false")
	}
}
