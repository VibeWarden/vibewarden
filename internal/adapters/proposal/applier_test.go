package proposal_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// stubReloader satisfies ports.ConfigReloader without doing any I/O.
type stubReloader struct{ reloaded bool }

func (r *stubReloader) Reload(_ context.Context, _ string) error {
	r.reloaded = true
	return nil
}

func (r *stubReloader) CurrentConfig() ports.RedactedConfig {
	return ports.RedactedConfig{}
}

func writeYAML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing yaml: %v", err)
	}
	return path
}

func TestApplier_BlockIP(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `
ip_filter:
  enabled: false
  mode: allowlist
  addresses: []
`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		ID:        "test",
		Type:      proposal.ActionBlockIP,
		Params:    map[string]any{"ip": "1.2.3.4"},
		Reason:    "test",
		Status:    proposal.StatusApproved,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Source:    proposal.SourceMCPAgent,
	}

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply block_ip: %v", err)
	}

	if !reloader.reloaded {
		t.Error("Reload was not called after apply")
	}

	raw, _ := os.ReadFile(cfgPath)
	content := string(raw)

	if !containsString(content, "1.2.3.4") {
		t.Errorf("config does not contain blocked IP: %s", content)
	}
	if !containsString(content, "blocklist") {
		t.Errorf("config mode not set to blocklist: %s", content)
	}
}

func TestApplier_BlockIPIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `
ip_filter:
  enabled: true
  mode: blocklist
  addresses:
    - 1.2.3.4
`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "1.2.3.4"},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply block_ip idempotent: %v", err)
	}

	// Should still reload even when IP was already present.
	if !reloader.reloaded {
		t.Error("Reload was not called")
	}
}

func TestApplier_BlockIP_MissingIP(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `{}`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err == nil {
		t.Fatal("expected error for missing ip param")
	}
}

func TestApplier_AdjustRateLimit(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `
rate_limit:
  enabled: true
  per_ip:
    requests_per_second: 10
    burst: 20
`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type: proposal.ActionAdjustRateLimit,
		Params: map[string]any{
			"requests_per_second": 5.0,
			"burst":               10,
		},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply adjust_rate_limit: %v", err)
	}

	if !reloader.reloaded {
		t.Error("Reload was not called")
	}

	raw, _ := os.ReadFile(cfgPath)
	content := string(raw)
	if !containsString(content, "5") {
		t.Errorf("config does not contain new rps: %s", content)
	}
}

func TestApplier_AdjustRateLimit_InvalidParams(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `{}`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"empty params", map[string]any{}},
		{"zero rps", map[string]any{"requests_per_second": 0.0}},
		{"zero burst", map[string]any{"burst": 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := proposal.Proposal{
				Type:   proposal.ActionAdjustRateLimit,
				Params: tt.params,
				Status: proposal.StatusApproved,
			}
			if err := applier.Apply(context.Background(), p); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestApplier_UpdateConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `
server:
  port: 8443
log:
  level: info
`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type: proposal.ActionUpdateConfig,
		Params: map[string]any{
			"log": map[string]any{"level": "debug"},
		},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply update_config: %v", err)
	}

	raw, _ := os.ReadFile(cfgPath)
	content := string(raw)
	if !containsString(content, "debug") {
		t.Errorf("config does not contain updated log level: %s", content)
	}
}

func TestApplier_UpdateConfig_EmptyParams(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `{}`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type:   proposal.ActionUpdateConfig,
		Params: map[string]any{},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err == nil {
		t.Fatal("expected error for empty update_config params")
	}
}

func TestApplier_UnknownActionType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeYAML(t, dir, "vibewarden.yaml", `{}`)

	reloader := &stubReloader{}
	applier := proposaladapter.NewApplier(cfgPath, reloader)

	p := proposal.Proposal{
		Type:   proposal.ActionType("unknown"),
		Params: map[string]any{},
		Status: proposal.StatusApproved,
	}

	if err := applier.Apply(context.Background(), p); err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
