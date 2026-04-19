package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestArtifact_AddTLS_BaseConfigUnchanged verifies that `vibew add tls --domain`
// does NOT modify vibewarden.yaml's tls.provider when it is already set to
// "self-signed". The domain should be written to vibewarden.production.yaml
// only; the base config's provider must remain unchanged.
//
// Regression test for #954.
func TestArtifact_AddTLS_BaseConfigUnchanged(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	// Create vibewarden.yaml with self-signed TLS.
	baseYAML := `server:
  port: 8443
tls:
  enabled: true
  provider: self-signed
upstream:
  host: "127.0.0.1"
  port: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Create vibewarden.production.yaml.
	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.production.yaml"), []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	// Run: vibew add tls --domain example.com <dir>
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"add", "tls", "--domain", "example.com", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("add tls failed: %v", err)
	}

	// Read vibewarden.yaml — provider MUST still be "self-signed".
	baseData, err := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading base config: %v", err)
	}
	baseStr := string(baseData)
	if !strings.Contains(baseStr, "provider: self-signed") && !strings.Contains(baseStr, "provider: \"self-signed\"") {
		t.Errorf("base config's tls.provider should still be 'self-signed', got:\n%s", baseStr)
	}

	// Read vibewarden.production.yaml — must contain the domain.
	prodData, err := os.ReadFile(filepath.Join(dir, "vibewarden.production.yaml"))
	if err != nil {
		t.Fatalf("reading prod config: %v", err)
	}
	prodStr := string(prodData)
	if !strings.Contains(prodStr, "example.com") {
		t.Errorf("production config should contain domain 'example.com', got:\n%s", prodStr)
	}
}

// TestArtifact_Init_GeneratesBothFiles verifies that `vibew init` creates both
// vibewarden.yaml and vibewarden.production.yaml with appropriate defaults.
func TestArtifact_Init_GeneratesBothFiles(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "bothfiles")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// vibewarden.yaml must exist.
	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist: %v", err)
	}

	// vibewarden.production.yaml must exist.
	prodPath := filepath.Join(projectDir, "vibewarden.production.yaml")
	if _, err := os.Stat(prodPath); err != nil {
		t.Errorf("expected vibewarden.production.yaml to exist: %v", err)
	}

	// vibewarden.production.yaml should have letsencrypt as default provider.
	prodData, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("reading prod config: %v", err)
	}
	if !strings.Contains(string(prodData), "letsencrypt") {
		t.Errorf("production config should default to letsencrypt, got:\n%s", string(prodData))
	}
}

// TestArtifact_Init_IgnoresHiddenDirs verifies that `vibew init` (without
// --force) does not fail when the only pre-existing entries in the directory
// are hidden directories like .claude/ and .git/. Hidden directories should
// be ignored in the "not empty" check.
//
// Regression test for #957.
func TestArtifact_Init_IgnoresHiddenDirs(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "hidden")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create hidden directories that should be ignored.
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init"})

	err = root.Execute()
	if err != nil {
		t.Errorf("init should succeed when directory only contains hidden dirs, got error: %v\noutput: %s", err, out.String())
	}
}
