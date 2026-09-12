package generate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/app/generate"
)

// kratosFlowToggles is the subset of the generated kratos.yml this test reads.
type kratosFlowToggles struct {
	Selfservice struct {
		Flows struct {
			Registration struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"registration"`
			Recovery struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"recovery"`
		} `yaml:"flows"`
	} `yaml:"selfservice"`
}

func flowTogglePtr(b bool) *bool { return &b }

// TestGenerate_KratosFlowTogglesFollowAuthUI verifies that
// auth.ui.show_registration / auth.ui.show_recovery drive the Kratos
// self-service flow toggles in the generated kratos.yml, so the built-in login
// page and Kratos can never disagree about which flows exist (#1512).
func TestGenerate_KratosFlowTogglesFollowAuthUI(t *testing.T) {
	tests := []struct {
		name             string
		showRegistration *bool
		showRecovery     *bool
		wantRegistration bool
		wantRecovery     bool
	}{
		{
			name:             "unset keeps both flows enabled",
			wantRegistration: true,
			wantRecovery:     true,
		},
		{
			name:             "registration disabled",
			showRegistration: flowTogglePtr(false),
			wantRegistration: false,
			wantRecovery:     true,
		},
		{
			name:             "recovery disabled",
			showRecovery:     flowTogglePtr(false),
			wantRegistration: true,
			wantRecovery:     false,
		},
		{
			name:             "both disabled",
			showRegistration: flowTogglePtr(false),
			showRecovery:     flowTogglePtr(false),
			wantRegistration: false,
			wantRecovery:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			cfg := minimalConfig()
			cfg.Auth.UI.ShowRegistration = tt.showRegistration
			cfg.Auth.UI.ShowRecovery = tt.showRecovery

			svc := generate.NewService(realRenderer())
			if err := svc.Generate(context.Background(), cfg.ToGeneratorInput(), outputDir); err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(outputDir, "kratos", "kratos.yml"))
			if err != nil {
				t.Fatalf("reading kratos.yml: %v", err)
			}

			var got kratosFlowToggles
			if err := yaml.Unmarshal(data, &got); err != nil {
				t.Fatalf("generated kratos.yml is not valid YAML: %v", err)
			}

			if got.Selfservice.Flows.Registration.Enabled != tt.wantRegistration {
				t.Errorf("selfservice.flows.registration.enabled = %v, want %v",
					got.Selfservice.Flows.Registration.Enabled, tt.wantRegistration)
			}
			if got.Selfservice.Flows.Recovery.Enabled != tt.wantRecovery {
				t.Errorf("selfservice.flows.recovery.enabled = %v, want %v",
					got.Selfservice.Flows.Recovery.Enabled, tt.wantRecovery)
			}
		})
	}
}
