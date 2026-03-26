package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// minimalVibeWardenYAML is a minimal valid vibewarden.yaml for CLI tests.
const minimalVibeWardenYAML = `# vibewarden.yaml
server:
  host: "127.0.0.1"
  port: 8080
upstream:
  host: "127.0.0.1"
  port: 3000
log:
  level: "info"
  format: "json"
security_headers:
  enabled: true
tls:
  enabled: false
`

func runAddCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	allArgs := append([]string{"add"}, args...)
	allArgs = append(allArgs, dir)
	root.SetArgs(allArgs)
	err := root.Execute()
	return out.String(), err
}

func TestAddAuthCmd(t *testing.T) {
	tests := []struct {
		name           string
		initial        string
		args           []string
		wantErr        bool
		wantOutContains string
		wantInYAML     []string
		notInYAML      []string
	}{
		{
			name:           "adds auth sections",
			initial:        minimalVibeWardenYAML,
			args:           []string{"auth"},
			wantOutContains: `"auth" enabled successfully`,
			wantInYAML:     []string{"kratos:", "auth:", "session_cookie_name:"},
		},
		{
			name:           "already enabled is a no-op with message",
			initial:        minimalVibeWardenYAML + "\nkratos:\n  public_url: \"http://localhost:4433\"\n",
			args:           []string{"auth"},
			wantOutContains: "already enabled",
		},
		{
			name:    "missing vibewarden.yaml returns error",
			initial: "", // no file written
			args:    []string{"auth"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.initial != "" {
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.initial), 0o644); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			out, err := runAddCmd(t, dir, tt.args...)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if tt.wantOutContains != "" && !strings.Contains(out, tt.wantOutContains) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutContains)
			}

			if len(tt.wantInYAML) > 0 || len(tt.notInYAML) > 0 {
				content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
				str := string(content)
				for _, want := range tt.wantInYAML {
					if !strings.Contains(str, want) {
						t.Errorf("vibewarden.yaml does not contain %q\n\n%s", want, str)
					}
				}
				for _, notWant := range tt.notInYAML {
					if strings.Contains(str, notWant) {
						t.Errorf("vibewarden.yaml should not contain %q\n\n%s", notWant, str)
					}
				}
			}
		})
	}
}

func TestAddRateLimitCmd(t *testing.T) {
	tests := []struct {
		name            string
		initial         string
		wantErr         bool
		wantOutContains string
		wantInYAML      []string
	}{
		{
			name:            "adds rate_limit section",
			initial:         minimalVibeWardenYAML,
			wantOutContains: `"rate-limiting" enabled successfully`,
			wantInYAML:      []string{"rate_limit:", "per_ip:", "exempt_paths:"},
		},
		{
			name:            "already enabled is no-op",
			initial:         minimalVibeWardenYAML + "\nrate_limit:\n  enabled: true\n",
			wantOutContains: "already enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.initial), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}

			out, err := runAddCmd(t, dir, "rate-limiting")

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantOutContains != "" && !strings.Contains(out, tt.wantOutContains) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutContains)
			}
			if len(tt.wantInYAML) > 0 {
				content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
				str := string(content)
				for _, want := range tt.wantInYAML {
					if !strings.Contains(str, want) {
						t.Errorf("vibewarden.yaml missing %q\n\n%s", want, str)
					}
				}
			}
		})
	}
}

func TestAddTLSCmd(t *testing.T) {
	tests := []struct {
		name            string
		initial         string
		args            []string
		wantErr         bool
		wantOutContains string
		wantInYAML      []string
	}{
		{
			name:            "adds tls section with domain",
			initial:         minimalVibeWardenYAML,
			args:            []string{"tls", "--domain", "example.com"},
			wantOutContains: `"tls" enabled successfully`,
			wantInYAML:      []string{"enabled: true", "example.com", "letsencrypt"},
		},
		{
			name:    "missing domain returns error",
			initial: minimalVibeWardenYAML,
			args:    []string{"tls"},
			wantErr: true,
		},
		{
			name:            "custom provider respected",
			initial:         minimalVibeWardenYAML,
			args:            []string{"tls", "--domain", "internal.corp", "--provider", "self-signed"},
			wantOutContains: `"tls" enabled successfully`,
			wantInYAML:      []string{"self-signed", "internal.corp"},
		},
		{
			name:            "already enabled tls is no-op",
			initial:         "tls:\n  enabled: true\n  domain: foo.com\n",
			args:            []string{"tls", "--domain", "bar.com"},
			wantOutContains: "already enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.initial != "" {
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.initial), 0o644); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			out, err := runAddCmd(t, dir, tt.args...)

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantOutContains != "" && !strings.Contains(out, tt.wantOutContains) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutContains)
			}
			if len(tt.wantInYAML) > 0 {
				content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
				str := string(content)
				for _, want := range tt.wantInYAML {
					if !strings.Contains(str, want) {
						t.Errorf("vibewarden.yaml missing %q\n\n%s", want, str)
					}
				}
			}
		})
	}
}

func TestAddAdminCmd(t *testing.T) {
	tests := []struct {
		name            string
		initial         string
		wantErr         bool
		wantOutContains string
		wantInYAML      []string
	}{
		{
			name:            "adds admin section",
			initial:         minimalVibeWardenYAML,
			wantOutContains: `"admin" enabled successfully`,
			wantInYAML:      []string{"admin:", "enabled: true", "VIBEWARDEN_ADMIN_TOKEN"},
		},
		{
			name:            "already enabled is no-op",
			initial:         minimalVibeWardenYAML + "\nadmin:\n  enabled: true\n",
			wantOutContains: "already enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.initial), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}

			out, err := runAddCmd(t, dir, "admin")

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantOutContains != "" && !strings.Contains(out, tt.wantOutContains) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutContains)
			}
			if len(tt.wantInYAML) > 0 {
				content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
				str := string(content)
				for _, want := range tt.wantInYAML {
					if !strings.Contains(str, want) {
						t.Errorf("vibewarden.yaml missing %q\n\n%s", want, str)
					}
				}
			}
		})
	}
}

func TestAddMetricsCmd(t *testing.T) {
	tests := []struct {
		name            string
		initial         string
		wantErr         bool
		wantOutContains string
		wantInYAML      []string
	}{
		{
			name:            "adds metrics section",
			initial:         minimalVibeWardenYAML,
			wantOutContains: `"metrics" enabled successfully`,
			wantInYAML:      []string{"metrics:", "enabled: true", "/metrics"},
		},
		{
			name:            "already enabled is no-op",
			initial:         minimalVibeWardenYAML + "\nmetrics:\n  enabled: true\n",
			wantOutContains: "already enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.initial), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}

			out, err := runAddCmd(t, dir, "metrics")

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.wantOutContains != "" && !strings.Contains(out, tt.wantOutContains) {
				t.Errorf("output %q does not contain %q", out, tt.wantOutContains)
			}
			if len(tt.wantInYAML) > 0 {
				content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
				str := string(content)
				for _, want := range tt.wantInYAML {
					if !strings.Contains(str, want) {
						t.Errorf("vibewarden.yaml missing %q\n\n%s", want, str)
					}
				}
			}
		})
	}
}

func TestAddCmd_AgentContextRegenerated(t *testing.T) {
	// When add succeeds, existing agent context files should be regenerated.
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(minimalVibeWardenYAML), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Pre-create agent context dirs so regeneration can write to them.
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	out, err := runAddCmd(t, dir, "auth")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Auth sections added.
	content, _ := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
	if !strings.Contains(string(content), "kratos:") {
		t.Error("vibewarden.yaml should contain kratos section")
	}

	// Output confirms success.
	if !strings.Contains(out, "enabled successfully") {
		t.Errorf("unexpected output: %s", out)
	}
}
