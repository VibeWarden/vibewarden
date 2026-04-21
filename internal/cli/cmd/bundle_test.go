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
	// Create a sites/<name> subdirectory WITH a vibewarden.yaml to trigger
	// the multi-site branch. An empty sites/blog/ no longer trips detection —
	// see TestBundleCmd_IsMultiSiteProject_TableDriven for the full matrix.
	siteDir := filepath.Join(dir, "sites", "blog")
	if err := os.MkdirAll(siteDir, 0o750); err != nil {
		t.Fatalf("mkdir sites/blog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("writing site yaml: %v", err)
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
	if !strings.Contains(err.Error(), "ADR-085") {
		t.Errorf("error should reference ADR-085 for tracking, got: %v", err)
	}
	// vibew deploy has been removed (ADR-086): the multi-site branch must
	// NOT direct users to a command that now exits 2.
	if strings.Contains(err.Error(), "vibew deploy") {
		t.Errorf("error must not reference the removed `vibew deploy` command, got: %v", err)
	}
}

// TestBundleCmd_IsMultiSiteProject_TableDriven covers the full detection
// matrix that mirrors internal/config/sites.LoadSites. The older detection
// (any subdir under sites/ trips it) was too permissive — an empty
// sites/blog/ made the bundle command hard-fail even though LoadSites
// would have silently skipped it.
func TestBundleCmd_IsMultiSiteProject_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, root string)
		wantMultiSite bool
	}{
		{
			name:          "no sites dir",
			setup:         func(t *testing.T, _ string) { t.Helper() },
			wantMultiSite: false,
		},
		{
			name: "empty sites dir",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites"), 0o750); err != nil {
					t.Fatalf("mkdir sites: %v", err)
				}
			},
			wantMultiSite: false,
		},
		{
			name: "sites/a without vibewarden.yaml",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites", "a"), 0o750); err != nil {
					t.Fatalf("mkdir sites/a: %v", err)
				}
			},
			wantMultiSite: false,
		},
		{
			name: "sites/a with vibewarden.yaml",
			setup: func(t *testing.T, root string) {
				t.Helper()
				site := filepath.Join(root, "sites", "a")
				if err := os.MkdirAll(site, 0o750); err != nil {
					t.Fatalf("mkdir sites/a: %v", err)
				}
				if err := os.WriteFile(filepath.Join(site, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing yaml: %v", err)
				}
			},
			wantMultiSite: true,
		},
		{
			name: "sites/a empty + sites/b populated (any populated wins)",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites", "a"), 0o750); err != nil {
					t.Fatalf("mkdir sites/a: %v", err)
				}
				siteB := filepath.Join(root, "sites", "b")
				if err := os.MkdirAll(siteB, 0o750); err != nil {
					t.Fatalf("mkdir sites/b: %v", err)
				}
				if err := os.WriteFile(filepath.Join(siteB, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing yaml: %v", err)
				}
			},
			wantMultiSite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupBundleProject(t)
			tt.setup(t, dir)

			root := cmd.NewRootCmd("test")
			outDir := filepath.Join(dir, "out")
			root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			err := root.Execute()

			gotMultiSite := err != nil && strings.Contains(err.Error(), "multi-site bundle is not yet supported")
			if gotMultiSite != tt.wantMultiSite {
				t.Errorf("multi-site detection = %v, want %v (err = %v)", gotMultiSite, tt.wantMultiSite, err)
			}
		})
	}
}

// TestDeriveProjectName_SanitisesAdversarialInput is the #1061 reviewer
// finding: deploy.sh interpolates the project name into a comment. Even
// comment context is not a safe place for arbitrary bytes to reach a
// shell — a crafted `name:` in vibewarden.yaml could smuggle in "` ; $()
// and friends. The bundle command strips every character outside the
// shell-safe subset [a-zA-Z0-9_-] from every candidate before returning.
func TestDeriveProjectName_SanitisesAdversarialInput(t *testing.T) {
	dir := setupBundleProject(t)

	// Overwrite vibewarden.yaml with an adversarial name. Quote-and-injection
	// attempt modelled after the reviewer's adversarial input.
	adversarial := "name: \"myproject\\\" && rm -rf /\"\nserver:\n  port: 8443\nupstream:\n  host: \"localhost\"\n  port: 3000\napp:\n  image: myapp:latest\n"
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(adversarial), 0o600); err != nil {
		t.Fatalf("writing adversarial config: %v", err)
	}

	root := cmd.NewRootCmd("test")
	outDir := filepath.Join(dir, "out")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v\noutput:\n%s", err, out.String())
	}

	body, err := os.ReadFile(filepath.Join(outDir, "deploy.sh"))
	if err != nil {
		t.Fatalf("reading deploy.sh: %v", err)
	}
	// deriveProjectName output appears on the comment line produced by
	// renderDeploySH: "# Reference deploy script generated by `vibew bundle`
	// for <name>." Extract just that line so assertions are narrowly scoped
	// (the rest of deploy.sh legitimately contains spaces, quotes, etc.).
	commentLine := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "# Reference deploy script generated by") {
			commentLine = line
			break
		}
	}
	if commentLine == "" {
		t.Fatalf("deploy.sh missing header comment, body:\n%s", body)
	}
	// None of the adversarial shell metacharacters must appear in the
	// comment where the sanitised project name is interpolated. If any of
	// these show up verbatim, the sanitiser is leaking attacker-controlled
	// bytes through into a shell file.
	for _, bad := range []string{"rm -rf", "&&", "\"", "/"} {
		if strings.Contains(commentLine, bad) {
			t.Errorf("deploy.sh comment line contains adversarial fragment %q\nline: %s", bad, commentLine)
		}
	}
	// The sanitiser is expected to produce "myprojectrm-rf" (letters, digits,
	// underscores, dashes survive; everything else is stripped). Assert the
	// non-empty safe prefix survived so we know sanitisation is not just
	// erasing the entire string.
	if !strings.Contains(commentLine, "myproject") {
		t.Errorf("sanitised name lost safe prefix\nline: %s", commentLine)
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
