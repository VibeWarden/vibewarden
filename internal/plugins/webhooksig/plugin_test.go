package webhooksig_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/webhooksig"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPlugin_Name(t *testing.T) {
	p := webhooksig.New(webhooksig.Config{}, discardLogger())
	if got := p.Name(); got != "webhook-signature" {
		t.Errorf("Name() = %q, want %q", got, "webhook-signature")
	}
}

func TestPlugin_InitDisabled(t *testing.T) {
	p := webhooksig.New(webhooksig.Config{Enabled: false}, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if h := p.Health(); !h.Healthy {
		t.Errorf("Health().Healthy = false, want true")
	}
	if got := p.ContributeCaddyHandlers(); got != nil {
		t.Errorf("ContributeCaddyHandlers() = %v, want nil when disabled", got)
	}
}

func TestPlugin_InitValid(t *testing.T) {
	t.Setenv("TEST_WEBHOOK_SECRET", "mysecret")

	cfg := webhooksig.Config{
		Enabled: true,
		Paths: []webhooksig.RuleConfig{
			{
				Path:         "/hooks/github",
				Provider:     "github",
				SecretEnvVar: "TEST_WEBHOOK_SECRET",
			},
		},
	}
	p := webhooksig.New(cfg, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if h := p.Health(); !h.Healthy {
		t.Errorf("Health().Healthy = false, want true")
	}
	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Errorf("ContributeCaddyHandlers() returned %d handlers, want 1", len(handlers))
	}
}

func TestPlugin_InitInvalidProvider(t *testing.T) {
	t.Setenv("TEST_WEBHOOK_SECRET", "mysecret")

	cfg := webhooksig.Config{
		Enabled: true,
		Paths: []webhooksig.RuleConfig{
			{
				Path:         "/hooks/test",
				Provider:     "nonexistent",
				SecretEnvVar: "TEST_WEBHOOK_SECRET",
			},
		},
	}
	p := webhooksig.New(cfg, discardLogger())
	if err := p.Init(context.Background()); err == nil {
		t.Error("Init() expected error for invalid provider, got nil")
	}
}

func TestPlugin_InitMissingPath(t *testing.T) {
	t.Setenv("TEST_WEBHOOK_SECRET", "mysecret")

	cfg := webhooksig.Config{
		Enabled: true,
		Paths: []webhooksig.RuleConfig{
			{
				Path:         "",
				Provider:     "github",
				SecretEnvVar: "TEST_WEBHOOK_SECRET",
			},
		},
	}
	p := webhooksig.New(cfg, discardLogger())
	if err := p.Init(context.Background()); err == nil {
		t.Error("Init() expected error for missing path, got nil")
	}
}

func TestPlugin_InitMissingSecretEnvVar(t *testing.T) {
	cfg := webhooksig.Config{
		Enabled: true,
		Paths: []webhooksig.RuleConfig{
			{
				Path:         "/hooks/test",
				Provider:     "github",
				SecretEnvVar: "",
			},
		},
	}
	p := webhooksig.New(cfg, discardLogger())
	if err := p.Init(context.Background()); err == nil {
		t.Error("Init() expected error for missing secret_env_var, got nil")
	}
}

func TestPlugin_StartStop(t *testing.T) {
	p := webhooksig.New(webhooksig.Config{Enabled: false}, discardLogger())
	_ = p.Init(context.Background())
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
	if h := p.Health(); h.Healthy {
		t.Error("Health().Healthy = true after Stop, want false")
	}
}

func TestPlugin_ContributeCaddyRoutes(t *testing.T) {
	p := webhooksig.New(webhooksig.Config{Enabled: true}, discardLogger())
	if routes := p.ContributeCaddyRoutes(); routes != nil {
		t.Errorf("ContributeCaddyRoutes() = %v, want nil", routes)
	}
}

func TestPlugin_AllProviders(t *testing.T) {
	tests := []struct {
		provider string
		extra    map[string]string // additional rule fields
	}{
		{provider: "stripe"},
		{provider: "github"},
		{provider: "slack"},
		{provider: "twilio"},
		{provider: "generic", extra: map[string]string{"header": "X-My-Sig"}},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			envVar := "TEST_SIG_SECRET_" + tt.provider
			t.Setenv(envVar, "testsecret")

			rule := webhooksig.RuleConfig{
				Path:         "/hooks/" + tt.provider,
				Provider:     tt.provider,
				SecretEnvVar: envVar,
			}
			if h, ok := tt.extra["header"]; ok {
				rule.Header = h
			}

			p := webhooksig.New(webhooksig.Config{
				Enabled: true,
				Paths:   []webhooksig.RuleConfig{rule},
			}, discardLogger())

			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error = %v, want nil for provider %q", err, tt.provider)
			}
			if h := p.Health(); !h.Healthy {
				t.Errorf("Health().Healthy = false, want true for provider %q", tt.provider)
			}
		})
	}
}

// Interface guard tests.
func TestPlugin_ImplementsInterfaces(t *testing.T) {
	p := webhooksig.New(webhooksig.Config{}, discardLogger())

	var _ ports.Plugin = p
	var _ ports.CaddyContributor = p
}
