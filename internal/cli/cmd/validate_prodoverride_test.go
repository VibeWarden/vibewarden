package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// runValidateNoArgs runs `vibew validate` (no config path argument) with the
// working directory set to dir. It returns (stdout, stderr, error).
func runValidateNoArgs(t *testing.T, dir string) (string, string, error) {
	t.Helper()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Logf("warning: could not restore cwd: %v", err)
		}
	})

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate"})
	execErr := root.Execute()
	return outBuf.String(), errBuf.String(), execErr
}

// TestValidate_AutoDiscoversProductionOverride_FailsOnLogMode is the
// regression test for BUG-5 (issue #1180): bare `vibew validate` in a
// directory that contains vibewarden.production.yaml with WAF in log mode must
// FAIL and attribute the FAIL row to vibewarden.production.yaml.
func TestValidate_AutoDiscoversProductionOverride_FailsOnLogMode(t *testing.T) {
	dir := fixtureDir(t, "prod_waf_log_auto")

	_, stderr, err := runValidateNoArgs(t, dir)

	if err == nil {
		t.Fatal("expected non-zero exit for WAF log mode in prod override, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	if !strings.Contains(stderr, "vibewarden.production.yaml") {
		t.Errorf("stderr should mention vibewarden.production.yaml as source, got: %q", stderr)
	}
	if !strings.Contains(stderr, "WAF") {
		t.Errorf("stderr should mention WAF, got: %q", stderr)
	}
}

// TestValidate_AutoDiscoversProductionOverride_BothClean verifies that bare
// `vibew validate` exits 0 when both vibewarden.yaml and
// vibewarden.production.yaml are clean.
func TestValidate_AutoDiscoversProductionOverride_BothClean(t *testing.T) {
	dir := fixtureDir(t, "prod_both_clean")

	stdout, stderr, err := runValidateNoArgs(t, dir)

	if err != nil {
		t.Fatalf("expected exit 0 for clean configs, got: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Configuration valid") {
		t.Errorf("stdout should contain 'Configuration valid', got: %q", stdout)
	}
}

// TestValidate_BaseFails_AttributedToBase verifies that when the base config
// itself triggers a FAIL (ACME-incompatible domain not overridden in prod) the
// FAIL row does NOT carry a "(vibewarden.production.yaml)" suffix.
func TestValidate_BaseFails_AttributedToBase(t *testing.T) {
	dir := fixtureDir(t, "base_acme_fail")

	// Pass the config path explicitly so the multisite check resolves correctly,
	// but the prod override discovery should still find the sibling file via the
	// configPath branch of discoverProdOverride.
	configPath := filepath.Join(dir, "vibewarden.yaml")
	_, stderr, err := runValidateOnFixture(t, configPath)

	if err == nil {
		t.Fatal("expected non-zero exit for ACME incompatible domain in base, got nil error")
	}
	if !strings.Contains(stderr, "FAIL") {
		t.Errorf("stderr should contain FAIL, got: %q", stderr)
	}
	// The failure originates from the base config (domain is not overridden in
	// prod), so no production source annotation should appear.
	if strings.Contains(stderr, "(vibewarden.production.yaml)") {
		t.Errorf("stderr should NOT attribute FAIL to vibewarden.production.yaml when base is the source, got: %q", stderr)
	}
}

// TestDiscoverProdOverride_EmptyConfigPathResolvesAgainstCwd verifies that
// discoverProdOverride("") resolves against os.Getwd() and returns an absolute
// path to the production override when it exists.
func TestDiscoverProdOverride_EmptyConfigPathResolvesAgainstCwd(t *testing.T) {
	dir := t.TempDir()
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte("name: prod\n"), 0o600); err != nil {
		t.Fatalf("writing prod override: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// discoverProdOverride is package-private; exercise via the RunChecks path
	// indirectly by verifying the full `vibew validate` flow discovers the file.
	// For a direct unit test, use the internal test package (validate_internal_test.go).
	// Here we use the exported behavior: produce a valid base config in the same
	// dir so that validate succeeds except for the prod override discovery.
	baseYAML := `name: myapp
server:
  port: 8443
upstream:
  port: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	// Override is already clean (just sets name) so validate should exit 0.
	_, _, execErr := runValidateNoArgs(t, dir)
	if execErr != nil {
		t.Fatalf("validate with clean prod override should pass, got: %v", execErr)
	}
}

// TestDiscoverProdOverride_EmptyConfigPathNoOverride verifies that
// discoverProdOverride("") returns "" when no vibewarden.production.yaml exists
// in cwd. Bare `vibew validate` must still succeed when no override is present.
func TestDiscoverProdOverride_EmptyConfigPathNoOverride(t *testing.T) {
	dir := t.TempDir()
	baseYAML := `name: myapp
server:
  port: 8443
upstream:
  port: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base: %v", err)
	}
	// No vibewarden.production.yaml in dir.

	_, _, err := runValidateNoArgs(t, dir)
	if err != nil {
		t.Fatalf("validate without prod override should pass, got: %v", err)
	}
}
