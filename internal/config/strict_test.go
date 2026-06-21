package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestLoadStrict_RejectsUnknownKeys is the #1053 / ADR-082 regression guard.
// Typos that silently loaded under v0.15.0 must now fail loudly via
// *UnknownKeyError with the file and offending key(s) named.
func TestLoadStrict_RejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		prod     string
		wantFile string // "base" or "prod" — selects which path must appear in the error
		wantKeys []string
	}{
		{
			name:     "unknown top-level in base",
			base:     "bogus_plugin:\n  x: 1\n",
			prod:     "",
			wantFile: "base",
			wantKeys: []string{"bogus_plugin"},
		},
		{
			name:     "typo under tls in prod",
			base:     "tls:\n  provider: self-signed\n",
			prod:     "tls:\n  dmain: foo\n",
			wantFile: "prod",
			wantKeys: []string{"tls.dmain"},
		},
		{
			name:     "multiple unknowns in prod sorted",
			base:     "tls:\n  provider: self-signed\n",
			prod:     "tls:\n  dmain: a\nunknown:\n  b: 1\n",
			wantFile: "prod",
			wantKeys: []string{"tls.dmain", "unknown"},
		},
		{
			name:     "unknown nested key under valid parent",
			base:     "tls:\n  cert_monitoring:\n    bogus: 1\n",
			prod:     "",
			wantFile: "base",
			wantKeys: []string{"tls.cert_monitoring.bogus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			basePath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(basePath, []byte(tt.base), 0o600); err != nil {
				t.Fatalf("writing base: %v", err)
			}
			var prodPath string
			if tt.prod != "" {
				prodPath = filepath.Join(dir, "vibewarden.production.yaml")
				if err := os.WriteFile(prodPath, []byte(tt.prod), 0o600); err != nil {
					t.Fatalf("writing prod: %v", err)
				}
			}

			_, err := config.LoadStrict(basePath, prodPath)
			if err == nil {
				t.Fatal("LoadStrict() returned nil, want *UnknownKeyError")
			}

			var unknown *config.UnknownKeyError
			if !errors.As(err, &unknown) {
				t.Fatalf("LoadStrict() err = %v (%T), want *UnknownKeyError", err, err)
			}

			wantPath := basePath
			if tt.wantFile == "prod" {
				wantPath = prodPath
			}
			if unknown.File != wantPath {
				t.Errorf("UnknownKeyError.File = %q, want %q", unknown.File, wantPath)
			}
			if len(unknown.Keys) != len(tt.wantKeys) {
				t.Fatalf("UnknownKeyError.Keys = %v, want %v", unknown.Keys, tt.wantKeys)
			}
			for i, k := range tt.wantKeys {
				if unknown.Keys[i] != k {
					t.Errorf("Keys[%d] = %q, want %q", i, unknown.Keys[i], k)
				}
			}

			// The error message must name the offending file and key for
			// a vibe coder to act on it.
			msg := unknown.Error()
			if !strings.Contains(msg, wantPath) {
				t.Errorf("Error() = %q, want it to contain %q", msg, wantPath)
			}
			for _, k := range tt.wantKeys {
				if !strings.Contains(msg, k) {
					t.Errorf("Error() = %q, want it to contain %q", msg, k)
				}
			}
		})
	}
}

// TestLoadStrict_AcceptsKnownKeys confirms strict mode does not over-reject:
// every field documented in the schema must pass through LoadStrict cleanly.
func TestLoadStrict_AcceptsKnownKeys(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	baseYAML := `server:
  port: 8443
tls:
  provider: self-signed
  email: ops@example.com
  acme_ca: https://acme.zerossl.com/v2/DV90
  cert_monitoring:
    enabled: true
rate_limit:
  enabled: true
`
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	prodYAML := `tls:
  provider: letsencrypt
  domain: example.com
  email: prod@example.com
`
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	cfg, err := config.LoadStrict(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadStrict() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadStrict() returned nil cfg without error")
	}
}

// TestLoadStrict_NoFiles returns defaults cleanly when neither file is
// provided. Used by tests and by callers that want the strict sibling of
// config.Load even without a config file on disk.
func TestLoadStrict_NoFiles(t *testing.T) {
	cfg, err := config.LoadStrict("", "")
	if err != nil {
		t.Fatalf("LoadStrict(\"\", \"\") error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadStrict() returned nil cfg")
	}
}

// TestLoadStrict_MissingProdOverride treats a non-existent override file the
// same as "no override", matching config.Load's lenient file handling.
func TestLoadStrict_MissingProdOverride(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "does-not-exist.yaml")

	cfg, err := config.LoadStrict(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadStrict() error = %v", err)
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8443)
	}
}

// TestUnknownKeyError_Error covers the small formatting helper directly so
// callers can rely on the message shape.
func TestUnknownKeyError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *config.UnknownKeyError
		want string
	}{
		{
			name: "file and single key",
			err:  &config.UnknownKeyError{File: "/tmp/vibewarden.yaml", Keys: []string{"tls.dmain"}},
			want: "config /tmp/vibewarden.yaml: unknown key(s): tls.dmain",
		},
		{
			name: "file and multiple keys",
			err:  &config.UnknownKeyError{File: "/tmp/vibewarden.yaml", Keys: []string{"a", "b"}},
			want: "config /tmp/vibewarden.yaml: unknown key(s): a, b",
		},
		{
			name: "empty file",
			err:  &config.UnknownKeyError{Keys: []string{"tls.dmain"}},
			want: "config: unknown key(s): tls.dmain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLoadStrict_DeployTargetPlatform_Accepted verifies that the new
// deploy.target_platform field is accepted by LoadStrict and does not
// trigger an UnknownKeyError. This ensures that production yaml files
// carrying this field are not rejected.
func TestLoadStrict_DeployTargetPlatform_Accepted(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	prodYAML := "server:\n  port: 443\ndeploy:\n  target_platform: linux/amd64\n"
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	cfg, err := config.LoadStrict(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadStrict() error = %v (deploy.target_platform must be accepted)", err)
	}
	if cfg.Deploy.TargetPlatform != "linux/amd64" {
		t.Errorf("Deploy.TargetPlatform = %q, want %q", cfg.Deploy.TargetPlatform, "linux/amd64")
	}
}

// TestLoadStrict_DeployUnknownKey_Rejected verifies that unknown sibling keys
// under the deploy namespace are rejected by LoadStrict.
func TestLoadStrict_DeployUnknownKey_Rejected(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	prodYAML := "deploy:\n  target_platform: linux/amd64\n  foo: bar\n"
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	_, err := config.LoadStrict(basePath, prodPath)
	if err == nil {
		t.Fatal("LoadStrict() returned nil, want error for deploy.foo")
	}

	var unknown *config.UnknownKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("want *UnknownKeyError, got %T: %v", err, err)
	}
	found := false
	for _, k := range unknown.Keys {
		if k == "deploy.foo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UnknownKeyError.Keys = %v, want to contain %q", unknown.Keys, "deploy.foo")
	}
}

// TestLoadStrict_HealthExposeVersion_Accepted verifies that health.expose_version
// is accepted by LoadStrict and does not trigger an UnknownKeyError. This
// ensures vibew validate / vibew bundle do not reject configs that set the
// new OWASP A05 hardening switch.
func TestLoadStrict_HealthExposeVersion_Accepted(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	baseYAML := "health:\n  expose_version: false\n"
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}

	_, err := config.LoadStrict(basePath, "")
	if err != nil {
		t.Fatalf("LoadStrict() error = %v (health.expose_version must be accepted)", err)
	}
}

// TestLoadStrict_DeployHost_Accepted verifies that the new deploy.host field
// is accepted by LoadStrict and does not trigger an UnknownKeyError. Production
// yaml files that carry this field must load cleanly.
func TestLoadStrict_DeployHost_Accepted(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	prodYAML := "server:\n  port: 443\ndeploy:\n  target_platform: linux/amd64\n  host: alice@host.example\n"
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}

	// LoadStrict validates the schema but only loads values from the base yaml
	// via config.Load (the prod yaml is only schema-checked, not merged into
	// cfg). The important assertion here is that LoadStrict does NOT return an
	// UnknownKeyError for deploy.host — the field is known and must be accepted.
	_, err := config.LoadStrict(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadStrict() error = %v (deploy.host must be accepted)", err)
	}
}
