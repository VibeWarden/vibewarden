package promptkickoff_test

// wrapper_script_test.go — end-to-end test for the release wrapper script.
//
// Shells out to scripts/release/emit-kickoff-artifacts.sh in a temporary
// directory and asserts that the artifacts it produces contain the same
// structural guarantees as the in-process forensic tests (release_artifact_test.go).
//
// This test is skipped on Windows (the wrapper is bash-only) and when the
// SKIP_WRAPPER_SCRIPT_TEST environment variable is set to "1" (useful for
// environments where building the binary is not available, e.g. some CI
// sandboxes).
//
// ADR-101.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the absolute path to the repository root. It resolves the
// root by walking up from the test file's directory until it finds a go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	// Use the test binary's working directory — go test sets this to the
	// package directory. Walk upward to find go.mod.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root (go.mod not found)")
		}
		dir = parent
	}
}

// TestWrapperScript_ArtifactsPassForensicChecks builds vibew, runs the
// release wrapper script, and asserts the emitted artifacts pass the same
// structural checks as the in-process forensic tests.
func TestWrapperScript_ArtifactsPassForensicChecks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wrapper script is bash-only; skipping on Windows")
	}
	if os.Getenv("SKIP_WRAPPER_SCRIPT_TEST") == "1" {
		t.Skip("SKIP_WRAPPER_SCRIPT_TEST=1")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()

	script := filepath.Join(root, "scripts", "release", "emit-kickoff-artifacts.sh")

	cmd := exec.Command(script, tmpDir) //nolint:gosec // script is a known path under the repo root
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("emit-kickoff-artifacts.sh failed: %v\n--- output ---\n%s", err, out)
	}
	t.Logf("script output:\n%s", out)

	for _, flavor := range []string{"dev", "deploy"} {
		path := filepath.Join(tmpDir, "agent-kickoff-"+flavor+".txt")
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		s := string(body)

		t.Run(flavor+"/header", func(t *testing.T) {
			if !strings.Contains(s, "VibeWarden Agent Kickoff Release Artifact") {
				t.Errorf("missing header sentinel")
			}
			if !strings.Contains(s, "Flavor: "+flavor) {
				t.Errorf("missing flavor line")
			}
			if !strings.Contains(s, "Canonical URL:") {
				t.Errorf("missing Canonical URL line")
			}
		})

		t.Run(flavor+"/placeholders", func(t *testing.T) {
			for _, ph := range []string{"{{prjname}}", "{{description}}"} {
				if !strings.Contains(s, ph) {
					t.Errorf("missing placeholder %q", ph)
				}
			}
			if flavor == "deploy" && !strings.Contains(s, "{{domain}}") {
				t.Errorf("deploy artifact missing {{domain}} placeholder")
			}
		})

		t.Run(flavor+"/no-sentinel-leak", func(t *testing.T) {
			if strings.Contains(s, "vwprjname") {
				t.Errorf("sentinel 'vwprjname' leaked into output")
			}
			if strings.Contains(s, "vwdomain.example.invalid") {
				t.Errorf("sentinel 'vwdomain.example.invalid' leaked into output")
			}
		})

		t.Run(flavor+"/optional-mkdir-annotation", func(t *testing.T) {
			if !strings.Contains(s, "# Skip if you're already in the project directory:") {
				t.Errorf("missing optional-mkdir annotation")
			}
		})
	}

	// Deploy-specific: post-#1138 and post-#1217 contract.
	deployPath := filepath.Join(tmpDir, "agent-kickoff-deploy.txt")
	deployBody, err := os.ReadFile(deployPath)
	if err != nil {
		t.Fatalf("read deploy artifact %s: %v", deployPath, err)
	}
	deploy := string(deployBody)

	required := []struct {
		name    string
		snippet string
	}{
		{"docker load -i image.tar", "docker load -i image.tar"},
		{"docker compose up -d", "docker compose up -d"},
		{"tar -czf dotfile-safe send", "tar -czf - -C"},
		{"tar -xzf dotfile-safe receive", "tar -xzf - -C"},
		{"healthcheck endpoint", "_vibewarden/health"},
	}
	for _, r := range required {
		t.Run("deploy/contains/"+r.name, func(t *testing.T) {
			if !strings.Contains(deploy, r.snippet) {
				t.Errorf("missing required snippet %q (post-deploy-contract drift)", r.snippet)
			}
		})
	}

	forbidden := []struct {
		name    string
		snippet string
	}{
		{"bash deploy.sh", "bash deploy.sh"},
		{"./deploy.sh", "./deploy.sh"},
		{"scp glob star", "scp -r .vibewarden/bundle/*"},
		{"scp glob dot", "scp -r .vibewarden/bundle/."},
	}
	for _, r := range forbidden {
		t.Run("deploy/absent/"+r.name, func(t *testing.T) {
			if strings.Contains(deploy, r.snippet) {
				t.Errorf("contains forbidden snippet %q (regression)", r.snippet)
			}
		})
	}
}
