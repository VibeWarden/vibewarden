package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// setupBundleProject prepares a t.TempDir scaffolded project with a
// vibewarden.yaml and AGENTS-VIBEWARDEN.md marker, then chdirs into it.
// The caller does not need to clean up — t.Cleanup restores the previous
// working directory.
func setupBundleProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeScaffoldingMarker(t, dir)
	writeVibewardenYAML(t, dir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	return dir
}

func TestBundleCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	bundleCmd, _, err := root.Find([]string{"bundle"})
	if err != nil {
		t.Fatalf("Find(bundle) error: %v", err)
	}
	if bundleCmd == nil {
		t.Fatal("expected 'bundle' command to be registered")
	}
}

func TestBundleCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	bundleCmd, _, err := root.Find([]string{"bundle"})
	if err != nil {
		t.Fatalf("Find(bundle) error: %v", err)
	}
	for _, name := range []string{"output", "overwrite", "image", "skip-image"} {
		if bundleCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s flag on bundle command", name)
		}
	}
}

func TestBundleCmd_Help_MentionsArtifacts(t *testing.T) {
	// The --help text must be self-describing for LLM agents per ADR-085.
	// This test is cheap, catches drift, and runs without any disk setup.
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	if err := root.Execute(); err != nil {
		t.Fatalf("bundle --help: %v", err)
	}
	help := out.String()
	for _, want := range []string{
		"docker-compose.yml",
		"sample.env",
		".env",
		"deploy.sh",
		"README.md",
		"image.tar",
		"--overwrite",
		"--skip-image",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("bundle --help missing %q", want)
		}
	}
}

func TestBundleCmd_MissingScaffolding(t *testing.T) {
	// Running vibew bundle in an un-scaffolded directory must fail with
	// the standard vibew init / vibew wrap hint, not a cryptic error.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error with no scaffolding, got nil")
	}
}

func TestBundleCmd_MultiSite_HardErrors(t *testing.T) {
	dir := setupBundleProject(t)
	// Create a sites/<name> subdirectory to trigger the multi-site branch.
	siteDir := filepath.Join(dir, "sites", "blog")
	if err := os.MkdirAll(siteDir, 0o750); err != nil {
		t.Fatalf("mkdir sites/blog: %v", err)
	}

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected multi-site error, got nil")
	}
	if !strings.Contains(err.Error(), "multi-site bundle is not yet supported") {
		t.Errorf("error missing multi-site message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "vibew deploy") {
		t.Errorf("error should direct users to vibew deploy, got: %v", err)
	}
}

func TestBundleCmd_ProducesBundle(t *testing.T) {
	dir := setupBundleProject(t)

	root := cmd.NewRootCmd("test")
	outDir := filepath.Join(dir, "out")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v\nstdout:\n%s", err, out.String())
	}

	for _, f := range []string{"vibewarden.yaml", "sample.env", ".env", "deploy.sh", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(outDir, f)); statErr != nil {
			t.Errorf("expected %s in bundle: %v", f, statErr)
		}
	}
	// image.tar must NOT be present because of --skip-image.
	if _, statErr := os.Stat(filepath.Join(outDir, "image.tar")); !os.IsNotExist(statErr) {
		t.Errorf("image.tar present despite --skip-image (stat err = %v)", statErr)
	}
}

func TestBundleCmd_DeploySHExecutable(t *testing.T) {
	dir := setupBundleProject(t)

	root := cmd.NewRootCmd("test")
	outDir := filepath.Join(dir, "out")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v", err)
	}

	info, err := os.Stat(filepath.Join(outDir, "deploy.sh"))
	if err != nil {
		t.Fatalf("stat deploy.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("deploy.sh not executable, mode = %v", info.Mode().Perm())
	}
}

func TestBundleCmd_DotEnvPreserved(t *testing.T) {
	dir := setupBundleProject(t)
	outDir := filepath.Join(dir, "out")

	// Run once.
	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out1 bytes.Buffer
	root1.SetOut(&out1)
	root1.SetErr(&out1)
	if err := root1.Execute(); err != nil {
		t.Fatalf("first bundle: %v\noutput:\n%s", err, out1.String())
	}

	// Simulate user edit to .env.
	dotEnv := filepath.Join(outDir, ".env")
	edited := []byte("VIBEWARDEN_APP_IMAGE=foo\nSTRIPE_KEY=sk_live_edited\n")
	if err := os.WriteFile(dotEnv, edited, 0o600); err != nil {
		t.Fatalf("writing edited .env: %v", err)
	}

	// Run twice — .env must survive without --overwrite.
	root2 := cmd.NewRootCmd("test")
	root2.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out2 bytes.Buffer
	root2.SetOut(&out2)
	root2.SetErr(&out2)
	if err := root2.Execute(); err != nil {
		t.Fatalf("second bundle: %v\noutput:\n%s", err, out2.String())
	}

	got, err := os.ReadFile(dotEnv) //nolint:gosec // test file
	if err != nil {
		t.Fatalf("reading .env after second bundle: %v", err)
	}
	if !bytes.Equal(got, edited) {
		t.Errorf(".env overwritten on second bundle\nwant: %q\ngot:  %q", edited, got)
	}
}

func TestBundleCmd_Overwrite_ReplacesDotEnv(t *testing.T) {
	dir := setupBundleProject(t)
	outDir := filepath.Join(dir, "out")

	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out1 bytes.Buffer
	root1.SetOut(&out1)
	root1.SetErr(&out1)
	if err := root1.Execute(); err != nil {
		t.Fatalf("first bundle: %v", err)
	}

	dotEnv := filepath.Join(outDir, ".env")

	edited := []byte("VIBEWARDEN_APP_IMAGE=foo\nSTRIPE_KEY=keepme\n")
	if err := os.WriteFile(dotEnv, edited, 0o600); err != nil {
		t.Fatalf("writing edited .env: %v", err)
	}

	root2 := cmd.NewRootCmd("test")
	root2.SetArgs([]string{"bundle", "--output", outDir, "--skip-image", "--overwrite"})
	var out2 bytes.Buffer
	root2.SetOut(&out2)
	root2.SetErr(&out2)
	if err := root2.Execute(); err != nil {
		t.Fatalf("second bundle: %v", err)
	}

	got, err := os.ReadFile(dotEnv) //nolint:gosec // test file
	if err != nil {
		t.Fatalf("reading .env after overwrite: %v", err)
	}
	if bytes.Contains(got, []byte("STRIPE_KEY=keepme")) {
		t.Errorf(".env still contains user edits despite --overwrite\ngot: %q", got)
	}
	// --overwrite must still produce a VIBEWARDEN_APP_IMAGE entry.
	if !bytes.Contains(got, []byte("VIBEWARDEN_APP_IMAGE=")) {
		t.Errorf(".env missing VIBEWARDEN_APP_IMAGE after --overwrite\ngot: %q", got)
	}
}
