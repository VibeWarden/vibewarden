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
		"README.md",
		"image.tar",
		"--overwrite",
		"--skip-image",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("bundle --help missing %q", want)
		}
	}
	if strings.Contains(help, "deploy.sh") {
		t.Errorf("bundle --help still mentions deploy.sh (removed in #1138)")
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
	if !strings.Contains(err.Error(), "multi-site bundle is post-v1") {
		t.Errorf("error missing multi-site message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "#1169") {
		t.Errorf("error should reference #1169 for tracking, got: %v", err)
	}
	// vibew deploy has been removed (ADR-086): the multi-site branch must
	// NOT direct users to a command that no longer exists (cobra now prints
	// `unknown command "deploy"`).
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

			gotMultiSite := err != nil && strings.Contains(err.Error(), "multi-site bundle is post-v1")
			if gotMultiSite != tt.wantMultiSite {
				t.Errorf("multi-site detection = %v, want %v (err = %v)", gotMultiSite, tt.wantMultiSite, err)
			}
		})
	}
}

// TestDeriveProjectName_SanitisesAdversarialInput is the #1061 reviewer
// finding retargeted at the README. The project name is now interpolated
// into the bundle README's heading. Even prose context is not a safe
// place for arbitrary bytes — a crafted `name:` in vibewarden.yaml could
// smuggle in shell metacharacters that would later end up in operator
// scripts that copy-paste the heading. The bundle command strips every
// character outside the shell-safe subset [a-zA-Z0-9_-] from every
// candidate before returning.
func TestDeriveProjectName_SanitisesAdversarialInput(t *testing.T) {
	dir := setupBundleProject(t)

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

	body, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	headingLine := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "# ") && strings.Contains(line, "deploy bundle") {
			headingLine = line
			break
		}
	}
	if headingLine == "" {
		t.Fatalf("README.md missing project heading, body:\n%s", body)
	}
	for _, bad := range []string{"rm -rf", "&&", "\"", "/"} {
		if strings.Contains(headingLine, bad) {
			t.Errorf("README.md heading contains adversarial fragment %q\nline: %s", bad, headingLine)
		}
	}
	if !strings.Contains(headingLine, "myproject") {
		t.Errorf("sanitised name lost safe prefix\nline: %s", headingLine)
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

	for _, f := range []string{"vibewarden.yaml", "sample.env", ".env", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(outDir, f)); statErr != nil {
			t.Errorf("expected %s in bundle: %v", f, statErr)
		}
	}
	// image.tar must NOT be present because of --skip-image.
	if _, statErr := os.Stat(filepath.Join(outDir, "image.tar")); !os.IsNotExist(statErr) {
		t.Errorf("image.tar present despite --skip-image (stat err = %v)", statErr)
	}
	// deploy.sh must NOT be present (#1138 dropped it).
	if _, statErr := os.Stat(filepath.Join(outDir, "deploy.sh")); !os.IsNotExist(statErr) {
		t.Errorf("deploy.sh present in bundle (removed in #1138)")
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

// TestRunBundle_ImageTagDerivation verifies that the image tag written into
// sample.env/.env matches the project name derived by deriveProjectName.
// Since v0.18.2, the derivation chain is: name: → dirname → "vibewarden".
// The App.Image derivation branch has been removed (#1199).
func TestRunBundle_ImageTagDerivation(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		dirName     string // subdirectory to run bundle in (cwd basename)
		wantImageIn string // expected IMAGE= prefix in sample.env
	}{
		{
			name:        "explicit name: takes precedence",
			yaml:        "name: myapp\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			dirName:     "qr-dali",
			wantImageIn: "VIBEWARDEN_APP_IMAGE=myapp-app:latest",
		},
		{
			name:        "dirname fallback when name: unset (app.image no longer used for project name)",
			yaml:        "server:\n  port: 8080\nupstream:\n  port: 3000\napp:\n  image: ghcr.io/org/webapp:v2\n",
			dirName:     "qr-dali",
			wantImageIn: "VIBEWARDEN_APP_IMAGE=qr-dali-app:latest",
		},
		{
			name:        "cwd-basename fallback when name: and app.image: both unset",
			yaml:        "server:\n  port: 8080\nupstream:\n  port: 3000\n",
			dirName:     "qr-dali",
			wantImageIn: "VIBEWARDEN_APP_IMAGE=qr-dali-app:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			projDir := filepath.Join(base, tt.dirName)
			if err := os.MkdirAll(projDir, 0o750); err != nil {
				t.Fatalf("mkdir %s: %v", projDir, err)
			}
			// Write scaffolding marker and config.
			writeScaffoldingMarker(t, projDir)
			if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(tt.yaml), 0o644); err != nil {
				t.Fatalf("writing vibewarden.yaml: %v", err)
			}

			// chdir into the project so bundle can find ./vibewarden.yaml.
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if err := os.Chdir(projDir); err != nil {
				t.Fatalf("chdir %s: %v", projDir, err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			outDir := filepath.Join(projDir, "out")
			root := cmd.NewRootCmd("test")
			root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); err != nil {
				t.Fatalf("bundle: %v\noutput:\n%s", err, out.String())
			}

			// Check sample.env.
			sampleEnv, err := os.ReadFile(filepath.Join(outDir, "sample.env"))
			if err != nil {
				t.Fatalf("reading sample.env: %v", err)
			}
			if !bytes.Contains(sampleEnv, []byte(tt.wantImageIn)) {
				t.Errorf("sample.env missing %q\ngot:\n%s", tt.wantImageIn, sampleEnv)
			}

			// Check .env.
			dotEnv, err := os.ReadFile(filepath.Join(outDir, ".env")) //nolint:gosec // test file
			if err != nil {
				t.Fatalf("reading .env: %v", err)
			}
			if !bytes.Contains(dotEnv, []byte(tt.wantImageIn)) {
				t.Errorf(".env missing %q\ngot:\n%s", tt.wantImageIn, dotEnv)
			}
		})
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

// TestBundle_Stdout_PrintsLiteralDeployCommands verifies that the "Next:
// deploy" block printed to stdout after a successful bundle contains the four
// literal deploy commands with app name and domain substituted.
//
// All cases pass --skip-image because the test environment has no Docker
// daemon. The non-skip-image path (docker load in stdout) is tested
// separately via TestBundle_Extras_Readme_FencedDeployBlock which uses
// the in-memory BundleFS and does not require docker. The assertions here
// cover: (a) the "Next: deploy" header is present, (b) app name and domain
// are substituted, (c) with --skip-image the docker load clause is absent.
func TestBundle_Stdout_PrintsLiteralDeployCommands(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantCmds     []string
		wantAbsent   []string
		spacedOutDir bool // when true, outDir path contains a space to exercise %q quoting
	}{
		{
			name: "app name and domain substituted in next block — no deploy.host uses bracketed placeholder",
			yaml: "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			wantCmds: []string{
				"Next: deploy",
				"ssh <your-ssh-user>@<your-ssh-host> 'mkdir -p /opt/myapp'",
				"tar -czf - -C",
				"| ssh <your-ssh-user>@<your-ssh-host> 'tar -xzf - -C /opt/myapp/'",
				"docker compose up -d",
				"curl -fsSL https://myapp.example.com/_vibewarden/health",
				// Hint paragraph must be present when deploy.host is unset.
				"Replace `<your-ssh-user>@<your-ssh-host>` with your actual SSH target.",
				"~/.ssh/config",
				"deploy.host: user@host",
			},
			// --skip-image is always passed; docker load must not appear.
			// The three banned artifact forms are guarded here so a regression
			// in bundle.go (stdout surface) is caught by this forensic check.
			// The old unbracketed placeholder forms are also forbidden (#1244).
			// The literal flag name "--print-deploy" must never leak into output (#1245).
			wantAbsent: []string{
				"docker load -i image.tar &&",
				"scp -r .vibewarden/bundle/*", // dotfile-eating glob eliminated by #1217
				"bash deploy.sh",              // already-removed artifact (#1138)
				"./deploy.sh",                 // ditto
				"user@<",                      // old unbracketed placeholder form (#1244)
				"user@host 'mkdir",            // pre-#1244 literal placeholder
				"--print-deploy",              // flag name must not leak into output (#1245)
			},
		},
		{
			name: "missing domain falls back to placeholder",
			yaml: "name: myapp\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			wantCmds: []string{
				"Next: deploy",
				"<your-domain>",
			},
			wantAbsent: []string{
				"scp -r .vibewarden/bundle/*",
				"bash deploy.sh",
				"./deploy.sh",
				"user@<",
				"--print-deploy",
			},
		},
		{
			// %q is the deliberate format verb for outDir in bundle.go. A future
			// change back to %s would break shell word-splitting for paths with
			// spaces. This case verifies that %q wraps the path in double-quotes
			// so the tar invocation is valid even when outDir contains a space.
			name:         "output path with spaces is double-quoted by %q",
			yaml:         "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			spacedOutDir: true,
			wantCmds: []string{
				"Next: deploy",
				// %q wraps the path in double-quotes; assert the opening quote is
				// present immediately after "-C " so a bare %s regression is caught.
				`tar -czf - -C "`,
			},
			wantAbsent: []string{
				"scp -r .vibewarden/bundle/*",
				"bash deploy.sh",
				"./deploy.sh",
				"user@<",
				"--print-deploy",
			},
		},
		{
			// deploy.host set in yaml substitutes verbatim; hint paragraph absent.
			name: "deploy.host set — substituted into Next block, hint absent",
			yaml: "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\ndeploy:\n  host: alice@host.example\n",
			wantCmds: []string{
				"Next: deploy",
				"ssh alice@host.example 'mkdir -p /opt/myapp'",
				"| ssh alice@host.example 'tar -xzf - -C /opt/myapp/'",
				`ssh alice@host.example "cd /opt/myapp`,
				"curl -fsSL https://myapp.example.com/_vibewarden/health",
			},
			wantAbsent: []string{
				"<your-ssh-user>@<your-ssh-host>",
				"Replace `<your-ssh-user>",
				"scp -r .vibewarden/bundle/*",
				"bash deploy.sh",
				"./deploy.sh",
				"user@<",
				"--print-deploy",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			projDir := filepath.Join(base, "testproject")
			if err := os.MkdirAll(projDir, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			writeScaffoldingMarker(t, projDir)
			if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(tt.yaml), 0o644); err != nil {
				t.Fatalf("writing vibewarden.yaml: %v", err)
			}

			origDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if err := os.Chdir(projDir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			// For the spaces case, use a subdirectory whose path contains a space
			// so that %q quoting in bundle.go is exercised. All other cases use
			// a simple "out" directory under projDir.
			var outDir string
			if tt.spacedOutDir {
				outDir = filepath.Join(base, "my project", "out")
				if err := os.MkdirAll(outDir, 0o750); err != nil {
					t.Fatalf("mkdir spaced outDir: %v", err)
				}
			} else {
				outDir = filepath.Join(projDir, "out")
			}

			root := cmd.NewRootCmd("test")
			// Always pass --skip-image: the test environment has no Docker daemon.
			// The non-skip-image stdout path (docker load clause) is covered by
			// TestBundle_Extras_Readme_FencedDeployBlock in bundle_extras_test.go.
			args := []string{"bundle", "--output", outDir, "--skip-image"}
			root.SetArgs(args)
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stdout)
			if err := root.Execute(); err != nil {
				t.Fatalf("bundle: %v\nstdout:\n%s", err, stdout.String())
			}

			out := stdout.String()
			for _, want := range tt.wantCmds {
				if !strings.Contains(out, want) {
					t.Errorf("stdout missing %q\nstdout:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("stdout must not contain %q\nstdout:\n%s", absent, out)
				}
			}
		})
	}
}

// TestBundle_PrintDeploy_FlagValidationErrors verifies that invalid --print-deploy
// flag combinations exit 1 with a clear error message before any bundle work
// runs. All seven invalid forms are driven through root.Execute() so the full
// cobra dispatch path is exercised.
func TestBundle_PrintDeploy_FlagValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains []string
	}{
		{
			name:        "--print-deploy + --host + --user (missing --path)",
			args:        []string{"bundle", "--skip-image", "--print-deploy", "--host", "h.example", "--user", "u"},
			errContains: []string{"--print-deploy requires", "--path"},
		},
		{
			name:        "--print-deploy + --host + --path (missing --user)",
			args:        []string{"bundle", "--skip-image", "--print-deploy", "--host", "h.example", "--path", "/opt/x"},
			errContains: []string{"--print-deploy requires", "--user"},
		},
		{
			name:        "--print-deploy + --user + --path (missing --host)",
			args:        []string{"bundle", "--skip-image", "--print-deploy", "--user", "u", "--path", "/opt/x"},
			errContains: []string{"--print-deploy requires", "--host"},
		},
		{
			name:        "--print-deploy only (all three missing)",
			args:        []string{"bundle", "--skip-image", "--print-deploy"},
			errContains: []string{"--print-deploy requires", "--host", "--user", "--path"},
		},
		{
			name:        "--host without --print-deploy",
			args:        []string{"bundle", "--skip-image", "--host", "h.example"},
			errContains: []string{"require --print-deploy"},
		},
		{
			name:        "--user without --print-deploy",
			args:        []string{"bundle", "--skip-image", "--user", "alice"},
			errContains: []string{"require --print-deploy"},
		},
		{
			name:        "--path without --print-deploy",
			args:        []string{"bundle", "--skip-image", "--path", "/opt/x"},
			errContains: []string{"require --print-deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validation runs before scaffolding checks (requireScaffolding comes
			// first in runBundle, but validatePrintDeployFlags is called right
			// after the outputDir default). Use a temp dir with scaffolding so the
			// test can observe the validation error, not the scaffolding error.
			dir := setupBundleProject(t)
			outDir := filepath.Join(dir, "out")

			root := cmd.NewRootCmd("test")
			args := append(tt.args, "--output", outDir)
			root.SetArgs(args)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for args %v, got nil\nstdout: %s", tt.args, out.String())
			}
			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q; got: %v", want, err)
				}
			}
		})
	}
}

// TestBundle_PrintDeploy_StdoutSubstitution verifies the three substitution
// scenarios for --print-deploy: all three values substituted, flag wins over
// deploy.host from config, and the bundle README is unaffected (Option A).
func TestBundle_PrintDeploy_StdoutSubstitution(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		prodYAML   string // empty = no production.yaml
		args       []string
		wantStdout []string
		wantAbsent []string
		// readmeCheck: if non-empty, assert the README does NOT contain this string.
		readmeNotContains []string
		// readmeContains: if non-empty, assert README DOES contain these strings.
		readmeContains []string
	}{
		{
			name:     "all three values substituted in stdout",
			yaml:     "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			prodYAML: "",
			args:     []string{"bundle", "--skip-image", "--print-deploy", "--host", "h.example", "--user", "alice", "--path", "/custom/foo"},
			wantStdout: []string{
				"ssh alice@h.example 'mkdir -p /custom/foo'",
				"| ssh alice@h.example 'tar -xzf - -C /custom/foo/'",
				`ssh alice@h.example "cd /custom/foo`,
				"curl -fsSL https://myapp.example.com/_vibewarden/health",
			},
			wantAbsent: []string{
				"<your-ssh-user>@<your-ssh-host>",
				"Replace `<your-ssh-user>",
				"/opt/myapp",
			},
		},
		{
			name:     "flag overrides deploy.host from production.yaml",
			yaml:     "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			prodYAML: "deploy:\n  host: root@configured.example\n",
			args:     []string{"bundle", "--skip-image", "--print-deploy", "--host", "h.example", "--user", "alice", "--path", "/custom/foo"},
			wantStdout: []string{
				"ssh alice@h.example",
				"/custom/foo",
			},
			wantAbsent: []string{
				"root@configured.example",
			},
			// README uses cfg.Deploy.Host (Option A — README is config-driven, not flag-driven).
			readmeNotContains: []string{"alice@h.example", "/custom/foo"},
			readmeContains:    []string{"root@configured.example"},
		},
		{
			name:     "README ignores flag (Option A) — placeholder when no production.yaml",
			yaml:     "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n",
			prodYAML: "",
			args:     []string{"bundle", "--skip-image", "--print-deploy", "--host", "h.example", "--user", "alice", "--path", "/custom/foo"},
			wantStdout: []string{
				"ssh alice@h.example 'mkdir -p /custom/foo'",
			},
			// README should not have the flag-injected values; it uses the placeholder.
			readmeNotContains: []string{"alice@h.example", "/custom/foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			projDir := filepath.Join(base, "testproject")
			if err := os.MkdirAll(projDir, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			writeScaffoldingMarker(t, projDir)
			if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(tt.yaml), 0o644); err != nil {
				t.Fatalf("writing vibewarden.yaml: %v", err)
			}
			if tt.prodYAML != "" {
				if err := os.WriteFile(filepath.Join(projDir, "vibewarden.production.yaml"), []byte(tt.prodYAML), 0o644); err != nil {
					t.Fatalf("writing vibewarden.production.yaml: %v", err)
				}
			}

			origDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if err := os.Chdir(projDir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			outDir := filepath.Join(projDir, "out")
			root := cmd.NewRootCmd("test")
			args := append(tt.args, "--output", outDir)
			root.SetArgs(args)
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stdout)
			if err := root.Execute(); err != nil {
				t.Fatalf("bundle: %v\nstdout:\n%s", err, stdout.String())
			}

			out := stdout.String()
			for _, want := range tt.wantStdout {
				if !strings.Contains(out, want) {
					t.Errorf("stdout missing %q\nstdout:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("stdout must not contain %q\nstdout:\n%s", absent, out)
				}
			}

			if len(tt.readmeNotContains) > 0 || len(tt.readmeContains) > 0 {
				readmeBytes, readErr := os.ReadFile(filepath.Join(outDir, "README.md"))
				if readErr != nil {
					t.Fatalf("reading README.md: %v", readErr)
				}
				readme := string(readmeBytes)
				for _, absent := range tt.readmeNotContains {
					if strings.Contains(readme, absent) {
						t.Errorf("README.md must not contain %q (README ignores --print-deploy flag)\nREADME:\n%s", absent, readme)
					}
				}
				for _, want := range tt.readmeContains {
					if !strings.Contains(readme, want) {
						t.Errorf("README.md missing %q\nREADME:\n%s", want, readme)
					}
				}
			}
		})
	}
}

// TestBundle_PrintDeploy_PathSubstitutesRemoteDir confirms that --path replaces
// the /opt/<appname> segment in all three SSH lines and that the healthcheck
// curl line (domain-based) is unaffected.
func TestBundle_PrintDeploy_PathSubstitutesRemoteDir(t *testing.T) {
	base := t.TempDir()
	projDir := filepath.Join(base, "testproject")
	if err := os.MkdirAll(projDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeScaffoldingMarker(t, projDir)
	yaml := "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n"
	if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	outDir := filepath.Join(projDir, "out")
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{
		"bundle", "--skip-image", "--output", outDir,
		"--print-deploy", "--host", "h.example", "--user", "u", "--path", "/custom/dir",
	})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v\nstdout:\n%s", err, stdout.String())
	}

	out := stdout.String()

	// All three SSH lines must use /custom/dir.
	for _, want := range []string{
		"'mkdir -p /custom/dir'",
		"'tar -xzf - -C /custom/dir/'",
		`"cd /custom/dir`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q — --path did not substitute remote dir\nstdout:\n%s", want, out)
		}
	}

	// The default path must not appear.
	if strings.Contains(out, "/opt/myapp") {
		t.Errorf("stdout still contains default /opt/myapp — --path substitution failed\nstdout:\n%s", out)
	}

	// The healthcheck curl line is domain-based, not path-based — must be unaffected.
	if !strings.Contains(out, "curl -fsSL https://myapp.example.com/_vibewarden/health") {
		t.Errorf("stdout missing healthcheck curl line\nstdout:\n%s", out)
	}
}

// TestBundle_PrintDeploy_HintParagraphSuppressed confirms that the hint
// paragraph ("Replace `<your-ssh-user>...`") is absent when --print-deploy
// is used with valid sub-flags.
func TestBundle_PrintDeploy_HintParagraphSuppressed(t *testing.T) {
	base := t.TempDir()
	projDir := filepath.Join(base, "testproject")
	if err := os.MkdirAll(projDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeScaffoldingMarker(t, projDir)
	yaml := "name: myapp\ntls:\n  domain: myapp.example.com\nserver:\n  port: 8080\nupstream:\n  port: 3000\n"
	if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	outDir := filepath.Join(projDir, "out")
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{
		"bundle", "--skip-image", "--output", outDir,
		"--print-deploy", "--host", "h.example", "--user", "alice", "--path", "/opt/myapp",
	})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v\nstdout:\n%s", err, stdout.String())
	}

	out := stdout.String()
	// The hint paragraph must be fully absent.
	for _, absent := range []string{
		"Replace `<your-ssh-user>",
		"~/.ssh/config",
		"deploy.host: user@host",
	} {
		if strings.Contains(out, absent) {
			t.Errorf("stdout must not contain hint paragraph fragment %q when --print-deploy is set\nstdout:\n%s", absent, out)
		}
	}
}

// TestBundle_Stdout_DomainFromProductionOverride is a regression test for #1215:
// `vibew bundle` must substitute tls.domain from vibewarden.production.yaml
// (the canonical place for letsencrypt domains via `vibew add tls --domain`)
// into the README and stdout deploy block. Before the fix the substitution
// only consulted the un-merged base config and rendered <your-domain> even
// when production.yaml had a real domain.
func TestBundle_Stdout_DomainFromProductionOverride(t *testing.T) {
	base := t.TempDir()
	projDir := filepath.Join(base, "prodproj")
	if err := os.MkdirAll(projDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeScaffoldingMarker(t, projDir)

	baseYAML := "name: prodproj\nserver:\n  port: 8443\nupstream:\n  port: 3000\n"
	if err := os.WriteFile(filepath.Join(projDir, "vibewarden.yaml"), []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	prodYAML := "tls:\n  domain: prod.example.com\n  provider: letsencrypt\n"
	if err := os.WriteFile(filepath.Join(projDir, "vibewarden.production.yaml"), []byte(prodYAML), 0o644); err != nil {
		t.Fatalf("writing vibewarden.production.yaml: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	outDir := filepath.Join(projDir, "out")
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"bundle", "--output", outDir, "--skip-image"})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle: %v\nstdout:\n%s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "https://prod.example.com/_vibewarden/health") {
		t.Errorf("stdout missing substituted domain from production.yaml\nstdout:\n%s", out)
	}
	if strings.Contains(out, "<your-domain>") {
		t.Errorf("stdout must not contain placeholder when production.yaml has tls.domain\nstdout:\n%s", out)
	}

	// README must also have the substituted domain.
	readmeBytes, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("read bundle README: %v", err)
	}
	readme := string(readmeBytes)
	if !strings.Contains(readme, "curl -fsSL https://prod.example.com/_vibewarden/health") {
		t.Errorf("bundle README missing substituted domain in fenced deploy block:\n%s", readme)
	}
	if strings.Contains(readme, "<your-domain>") {
		t.Errorf("bundle README must not contain placeholder when production.yaml has tls.domain")
	}
}
