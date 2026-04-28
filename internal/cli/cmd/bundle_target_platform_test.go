package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// setupBundleProjectWithProdYAML is like setupBundleProject but also writes a
// vibewarden.production.yaml with the given content alongside vibewarden.yaml.
func setupBundleProjectWithProdYAML(t *testing.T, prodYAML string) string {
	t.Helper()
	dir := setupBundleProject(t)
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing vibewarden.production.yaml: %v", err)
	}
	return dir
}

// TestBundleCmd_TargetPlatform_FlagRegistered verifies that --target-platform
// is a registered flag on the bundle command.
func TestBundleCmd_TargetPlatform_FlagRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	bundleCmd, _, err := root.Find([]string{"bundle"})
	if err != nil {
		t.Fatalf("Find(bundle) error: %v", err)
	}
	f := bundleCmd.Flags().Lookup("target-platform")
	if f == nil {
		t.Fatal("expected --target-platform flag on bundle command")
	}
	// Default must be empty so we can detect "user did not pass flag".
	if f.DefValue != "" {
		t.Errorf("--target-platform default = %q, want %q (empty sentinel)", f.DefValue, "")
	}
}

// TestBundleCmd_NoYAML_NoFlag_DefaultsToAmd64 verifies that when no
// vibewarden.production.yaml exists and no --target-platform flag is passed,
// the bundle succeeds (default linux/amd64 is used). The actual image is not
// inspected because --skip-image is set; we are testing the flag resolution path.
func TestBundleCmd_NoYAML_NoFlag_DefaultsToAmd64(t *testing.T) {
	dir := setupBundleProject(t)
	outDir := filepath.Join(dir, "out")

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err != nil {
		t.Fatalf("bundle without flag or yaml should succeed (--skip-image): %v\noutput: %s", err, out.String())
	}
}

// TestBundleCmd_YAMLTargetPlatform_AcceptedByStrict verifies that a
// production.yaml with deploy.target_platform is accepted by LoadStrict
// (i.e., the field is not rejected as unknown). The bundle then succeeds
// because --skip-image bypasses the image inspection.
func TestBundleCmd_YAMLTargetPlatform_AcceptedByStrict(t *testing.T) {
	prodYAML := "server:\n  port: 443\ndeploy:\n  target_platform: linux/amd64\n"
	dir := setupBundleProjectWithProdYAML(t, prodYAML)
	outDir := filepath.Join(dir, "out")

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("bundle with deploy.target_platform in yaml should succeed: %v\noutput: %s", err, out.String())
	}
}

// TestBundleCmd_YAMLUnknownDeployKey_RejectedByStrict verifies that a
// production.yaml with an unknown deploy.* key is rejected by LoadStrict
// before any files are written.
func TestBundleCmd_YAMLUnknownDeployKey_RejectedByStrict(t *testing.T) {
	prodYAML := "deploy:\n  target_platform: linux/amd64\n  bogus_key: oops\n"
	dir := setupBundleProjectWithProdYAML(t, prodYAML)
	outDir := filepath.Join(dir, "out")

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown deploy.bogus_key, got nil")
	}
	if !strings.Contains(err.Error(), "bogus_key") && !strings.Contains(out.String(), "bogus_key") {
		t.Errorf("error/output should mention unknown key 'bogus_key'\nerr: %v\nout: %s", err, out.String())
	}
}
