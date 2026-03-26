package yamlmod_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// minimalConfig is a realistic minimal vibewarden.yaml produced by
// `vibewarden init`.
const minimalConfig = `# vibewarden.yaml
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

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}
	return path
}

func TestToggler_ReadFeatures(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    scaffold.FeatureState
		wantErr bool
	}{
		{
			name: "minimal config — all features disabled",
			yaml: minimalConfig,
			want: scaffold.FeatureState{
				UpstreamPort:     3000,
				AuthEnabled:      false,
				RateLimitEnabled: false,
				TLSEnabled:       false,
				AdminEnabled:     false,
				MetricsEnabled:   false,
			},
		},
		{
			name: "auth enabled via kratos key",
			yaml: minimalConfig + "\nkratos:\n  public_url: \"http://localhost:4433\"\n",
			want: scaffold.FeatureState{
				UpstreamPort: 3000,
				AuthEnabled:  true,
			},
		},
		{
			name: "auth enabled via auth key",
			yaml: minimalConfig + "\nauth:\n  session_cookie_name: \"s\"\n",
			want: scaffold.FeatureState{
				UpstreamPort: 3000,
				AuthEnabled:  true,
			},
		},
		{
			name: "rate limit enabled",
			yaml: minimalConfig + "\nrate_limit:\n  enabled: true\n",
			want: scaffold.FeatureState{
				UpstreamPort:     3000,
				RateLimitEnabled: true,
			},
		},
		{
			name: "tls enabled",
			yaml: "server:\n  port: 8080\nupstream:\n  port: 4000\ntls:\n  enabled: true\n",
			want: scaffold.FeatureState{
				UpstreamPort: 4000,
				TLSEnabled:   true,
			},
		},
		{
			name: "admin enabled",
			yaml: minimalConfig + "\nadmin:\n  enabled: true\n",
			want: scaffold.FeatureState{
				UpstreamPort: 3000,
				AdminEnabled: true,
			},
		},
		{
			name: "metrics enabled",
			yaml: minimalConfig + "\nmetrics:\n  enabled: true\n",
			want: scaffold.FeatureState{
				UpstreamPort:   3000,
				MetricsEnabled: true,
			},
		},
		{
			name:    "missing file returns ErrConfigNotFound",
			wantErr: true,
		},
	}

	tog := yamlmod.NewToggler()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			var path string
			if tt.yaml != "" {
				path = writeConfig(t, dir, tt.yaml)
			} else {
				path = filepath.Join(dir, "vibewarden.yaml") // does not exist
			}

			got, err := tog.ReadFeatures(ctx, path)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, scaffold.ErrConfigNotFound) {
					t.Errorf("expected ErrConfigNotFound, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.UpstreamPort != tt.want.UpstreamPort {
				t.Errorf("UpstreamPort = %d, want %d", got.UpstreamPort, tt.want.UpstreamPort)
			}
			if got.AuthEnabled != tt.want.AuthEnabled {
				t.Errorf("AuthEnabled = %v, want %v", got.AuthEnabled, tt.want.AuthEnabled)
			}
			if got.RateLimitEnabled != tt.want.RateLimitEnabled {
				t.Errorf("RateLimitEnabled = %v, want %v", got.RateLimitEnabled, tt.want.RateLimitEnabled)
			}
			if got.TLSEnabled != tt.want.TLSEnabled {
				t.Errorf("TLSEnabled = %v, want %v", got.TLSEnabled, tt.want.TLSEnabled)
			}
			if got.AdminEnabled != tt.want.AdminEnabled {
				t.Errorf("AdminEnabled = %v, want %v", got.AdminEnabled, tt.want.AdminEnabled)
			}
			if got.MetricsEnabled != tt.want.MetricsEnabled {
				t.Errorf("MetricsEnabled = %v, want %v", got.MetricsEnabled, tt.want.MetricsEnabled)
			}
		})
	}
}

func TestToggler_EnableFeature(t *testing.T) {
	tests := []struct {
		name        string
		initial     string
		feature     scaffold.Feature
		opts        scaffold.FeatureOptions
		wantErr     bool
		wantErrIs   error
		wantInYAML  []string
		preserveStr string // string that must survive in the output
	}{
		{
			name:       "enable auth adds kratos and auth sections",
			initial:    minimalConfig,
			feature:    scaffold.FeatureAuth,
			wantInYAML: []string{"kratos:", "public_url:", "auth:", "session_cookie_name:"},
		},
		{
			name:      "enable auth twice returns ErrFeatureAlreadyEnabled",
			initial:   minimalConfig + "\nkratos:\n  public_url: \"http://localhost:4433\"\n",
			feature:   scaffold.FeatureAuth,
			wantErr:   true,
			wantErrIs: scaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name:       "enable rate-limiting adds rate_limit section",
			initial:    minimalConfig,
			feature:    scaffold.FeatureRateLimit,
			wantInYAML: []string{"rate_limit:", "per_ip:", "requests_per_second:", "exempt_paths:"},
		},
		{
			name:      "enable rate-limiting twice returns ErrFeatureAlreadyEnabled",
			initial:   minimalConfig + "\nrate_limit:\n  enabled: true\n",
			feature:   scaffold.FeatureRateLimit,
			wantErr:   true,
			wantErrIs: scaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name:    "enable tls adds tls section with domain and provider",
			initial: minimalConfig,
			feature: scaffold.FeatureTLS,
			opts: scaffold.FeatureOptions{
				TLSDomain:   "example.com",
				TLSProvider: "letsencrypt",
			},
			wantInYAML: []string{"tls:", "enabled: true", "example.com", "letsencrypt"},
		},
		{
			name:    "enable tls defaults provider to letsencrypt",
			initial: minimalConfig,
			feature: scaffold.FeatureTLS,
			opts:    scaffold.FeatureOptions{TLSDomain: "foo.com"},
			wantInYAML: []string{"enabled: true", "letsencrypt"},
		},
		{
			name:      "enable tls when already enabled returns ErrFeatureAlreadyEnabled",
			initial:   "tls:\n  enabled: true\n  domain: foo.com\n",
			feature:   scaffold.FeatureTLS,
			wantErr:   true,
			wantErrIs: scaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name:       "enable admin adds admin section",
			initial:    minimalConfig,
			feature:    scaffold.FeatureAdmin,
			wantInYAML: []string{"admin:", "enabled: true", "VIBEWARDEN_ADMIN_TOKEN"},
		},
		{
			name:      "enable admin twice returns ErrFeatureAlreadyEnabled",
			initial:   minimalConfig + "\nadmin:\n  enabled: true\n",
			feature:   scaffold.FeatureAdmin,
			wantErr:   true,
			wantErrIs: scaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name:       "enable metrics adds metrics section",
			initial:    minimalConfig,
			feature:    scaffold.FeatureMetrics,
			wantInYAML: []string{"metrics:", "enabled: true", "/metrics"},
		},
		{
			name:      "enable metrics twice returns ErrFeatureAlreadyEnabled",
			initial:   minimalConfig + "\nmetrics:\n  enabled: true\n",
			feature:   scaffold.FeatureMetrics,
			wantErr:   true,
			wantErrIs: scaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name:         "comments are preserved after auth enable",
			initial:      "# top comment\nserver:\n  port: 8080\nupstream:\n  port: 3000\ntls:\n  enabled: false\n",
			feature:      scaffold.FeatureAuth,
			preserveStr:  "# top comment",
			wantInYAML:   []string{"kratos:"},
		},
		{
			name:    "unknown feature returns error",
			initial: minimalConfig,
			feature: scaffold.Feature("unknown"),
			wantErr: true,
		},
		{
			name:    "missing config returns ErrConfigNotFound",
			feature: scaffold.FeatureAuth,
			wantErr: true,
		},
	}

	tog := yamlmod.NewToggler()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			var path string
			if tt.initial != "" {
				path = writeConfig(t, dir, tt.initial)
			} else {
				path = filepath.Join(dir, "vibewarden.yaml")
			}

			err := tog.EnableFeature(ctx, path, tt.feature, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("expected errors.Is(%v), got %v", tt.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("reading updated file: %v", readErr)
			}
			str := string(content)

			for _, want := range tt.wantInYAML {
				if !strings.Contains(str, want) {
					t.Errorf("output does not contain %q\n\nContent:\n%s", want, str)
				}
			}
			if tt.preserveStr != "" && !strings.Contains(str, tt.preserveStr) {
				t.Errorf("output lost preserved string %q\n\nContent:\n%s", tt.preserveStr, str)
			}
		})
	}
}

func TestToggler_EnableFeature_Idempotent(t *testing.T) {
	// Calling EnableFeature on an already-enabled feature must return
	// ErrFeatureAlreadyEnabled — it must NOT silently modify the file.
	tog := yamlmod.NewToggler()
	ctx := context.Background()

	dir := t.TempDir()
	path := writeConfig(t, dir, minimalConfig+"\nkratos:\n  public_url: \"http://localhost:4433\"\n")

	original, _ := os.ReadFile(path)

	err := tog.EnableFeature(ctx, path, scaffold.FeatureAuth, scaffold.FeatureOptions{})
	if !errors.Is(err, scaffold.ErrFeatureAlreadyEnabled) {
		t.Fatalf("expected ErrFeatureAlreadyEnabled, got %v", err)
	}

	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Error("file was modified even though feature was already enabled")
	}
}
