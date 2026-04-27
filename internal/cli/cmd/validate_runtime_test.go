package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// fixtureDir returns the absolute path to a fixture under testdata/validate/.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "validate", name))
	if err != nil {
		t.Fatalf("resolving fixture path: %v", err)
	}
	return abs
}

// runValidateOnFixture runs `vibew validate <configPath>` and returns
// (stdout, stderr, error).
func runValidateOnFixture(t *testing.T, configPath string) (string, string, error) {
	t.Helper()
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", configPath})
	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// TestValidateRuntime_NameCollision verifies Check 1: when the config has no
// name: set and the project directory is named "vibewarden", validate FAIL.
// The fixture directory is named "name_collision" so we must rename a tempdir
// to "vibewarden" and copy the fixture config into it.
func TestValidateRuntime_NameCollision(t *testing.T) {
	// Prepare a tempdir with a "vibewarden" subdirectory that contains the fixture config.
	parent := t.TempDir()
	vibeDir := filepath.Join(parent, "vibewarden")
	if err := os.Mkdir(vibeDir, 0o750); err != nil {
		t.Fatalf("mkdir vibewarden: %v", err)
	}

	fixtureConfig := filepath.Join(fixtureDir(t, "name_collision"), "vibewarden.yaml")
	data, err := os.ReadFile(fixtureConfig)
	if err != nil {
		t.Fatalf("reading fixture config: %v", err)
	}
	configPath := filepath.Join(vibeDir, "vibewarden.yaml")
	if err := os.WriteFile(configPath, data, 0o600); err != nil { //nolint:gosec // test: configPath is built from t.TempDir() + known filename
		t.Fatalf("writing config: %v", err)
	}

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for name collision, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "vibewarden") {
		t.Errorf("stderr should contain the directory name, got: %q", stderr)
	}
	if !strings.Contains(stderr, "collides") {
		t.Errorf("stderr should contain 'collides', got: %q", stderr)
	}
}

// TestValidateRuntime_DockerfileMismatch verifies Check 2: Dockerfile EXPOSE
// does not match upstream.port.
func TestValidateRuntime_DockerfileMismatch(t *testing.T) {
	dir := fixtureDir(t, "dockerfile_mismatch")
	configPath := filepath.Join(dir, "vibewarden.yaml")

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for Dockerfile mismatch, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "EXPOSE") {
		t.Errorf("stderr should contain 'EXPOSE', got: %q", stderr)
	}
	if !strings.Contains(stderr, "upstream.port") {
		t.Errorf("stderr should contain 'upstream.port', got: %q", stderr)
	}
}

// TestValidateRuntime_ImageTagDrift verifies Check 3: .env VIBEWARDEN_APP_IMAGE
// drift from the expected tag.
func TestValidateRuntime_ImageTagDrift(t *testing.T) {
	dir := fixtureDir(t, "image_tag_drift")
	configPath := filepath.Join(dir, "vibewarden.yaml")

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for image-tag drift, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "VIBEWARDEN_APP_IMAGE") {
		t.Errorf("stderr should contain VIBEWARDEN_APP_IMAGE, got: %q", stderr)
	}
	if !strings.Contains(stderr, "vibew bundle --overwrite") {
		t.Errorf("stderr should contain 'vibew bundle --overwrite', got: %q", stderr)
	}
}

// TestValidateRuntime_ACMELocalhost verifies Check 4: ACME provider configured
// with localhost domain.
func TestValidateRuntime_ACMELocalhost(t *testing.T) {
	dir := fixtureDir(t, "acme_localhost")
	configPath := filepath.Join(dir, "vibewarden.yaml")

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for ACME localhost, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "localhost") {
		t.Errorf("stderr should contain 'localhost', got: %q", stderr)
	}
	if !strings.Contains(stderr, "Let's Encrypt") {
		t.Errorf("stderr should mention Let's Encrypt, got: %q", stderr)
	}
}

// TestValidateRuntime_WAFProdLog verifies Check 5: WAF enabled with mode: log
// in the production config overlay.
func TestValidateRuntime_WAFProdLog(t *testing.T) {
	dir := fixtureDir(t, "waf_prod_log")
	configPath := filepath.Join(dir, "vibewarden.yaml")

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for WAF prod-log mode, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "WAF") {
		t.Errorf("stderr should contain 'WAF', got: %q", stderr)
	}
	if !strings.Contains(stderr, "mode: log") {
		t.Errorf("stderr should contain 'mode: log', got: %q", stderr)
	}
}

// TestValidateRuntime_WAFProdLogAcknowledged verifies that when
// waf.acknowledge_log_mode: true is set the check emits OK instead of FAIL.
func TestValidateRuntime_WAFProdLogAcknowledged(t *testing.T) {
	dir := t.TempDir()
	baseYAML := `name: myapp
server:
  port: 8443
upstream:
  port: 3000
`
	prodYAML := `waf:
  enabled: true
  mode: log
  acknowledge_log_mode: true
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.production.yaml"), []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod: %v", err)
	}
	configPath := filepath.Join(dir, "vibewarden.yaml")

	_, stderr, err := runValidateOnFixture(t, configPath)

	if err != nil {
		t.Fatalf("expected success with acknowledged WAF log mode, got: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "OK") {
		t.Errorf("stderr should contain OK row for acknowledged log mode, got: %q", stderr)
	}
	if !strings.Contains(stderr, "WAF log-mode acknowledged") {
		t.Errorf("stderr should contain acknowledgement message, got: %q", stderr)
	}
}
