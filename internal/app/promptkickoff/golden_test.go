package promptkickoff_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/promptkickoff"
	clitemplates "github.com/vibewarden/vibewarden/internal/cli/templates"
)

// update controls whether golden files are regenerated on this run.
// Set via: go test ./... -update (parsed manually below) or via environment
// variable UPDATE_GOLDEN=1.
var updateGolden = os.Getenv("UPDATE_GOLDEN") == "1"

// fixtureOpts is the canonical input for golden-file generation. Version is
// pinned to "v0.0.0-test" so the rendered output is deterministic across builds.
func fixtureOpts(deploy bool) promptkickoff.Options {
	return promptkickoff.Options{
		Name:         "foo",
		Describe:     "bar",
		Domain:       "demo.example.com",
		VibewVersion: "v0.0.0-test",
		Deploy:       deploy,
	}
}

func newService(t *testing.T) *promptkickoff.Service {
	t.Helper()
	renderer := templateadapter.NewRenderer(clitemplates.FS)
	return promptkickoff.NewService(renderer)
}

func TestGolden_DevFlavor(t *testing.T) {
	svc := newService(t)
	got, err := svc.Render(fixtureOpts(false))
	if err != nil {
		t.Fatalf("Render(dev) error: %v", err)
	}
	compareGolden(t, "testdata/dev.golden", got)
}

func TestGolden_DeployFlavor(t *testing.T) {
	svc := newService(t)
	got, err := svc.Render(fixtureOpts(true))
	if err != nil {
		t.Fatalf("Render(deploy) error: %v", err)
	}
	compareGolden(t, "testdata/deploy.golden", got)
}

// compareGolden compares got against the golden file at path.
// When updateGolden is true, the golden file is written with got instead.
func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}
		t.Logf("updated golden file %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v\n(run with UPDATE_GOLDEN=1 to generate)", path, err)
	}

	if string(got) != string(want) {
		t.Errorf("golden mismatch for %s\n\n--- want ---\n%s\n--- got ---\n%s",
			path, want, got)
	}
}

// ---- deploy-recipe forensic tests -------------------------------------------

// TestDeployTemplate_ContainsRequiredCommands is a forensic consistency test
// (ADR-099 §Test strategy). It asserts the deploy template contains the four
// landmark commands that constitute the canonical deploy recipe (ADR-099).
// The bundle README is prose-only (see TestBundle_Extras_Readme_DeployContract);
// ADR-099 is the authoritative source for this command sequence.
// If a deploy-contract change ever updates the template without updating these
// assertions, the test fails immediately rather than silently shipping the
// wrong instructions to agents.
func TestDeployTemplate_ContainsRequiredCommands(t *testing.T) {
	svc := newService(t)
	out, err := svc.Render(fixtureOpts(true))
	if err != nil {
		t.Fatalf("Render(deploy) error: %v", err)
	}
	content := string(out)

	required := []struct {
		name    string
		snippet string
	}{
		{"tar -czf", "tar -czf - -C"},
		{"tar -xzf", "tar -xzf - -C"},
		{"docker load -i image.tar", "docker load -i image.tar"},
		{"docker compose up -d", "docker compose up -d"},
		{"healthcheck curl", "curl -fsSL https://demo.example.com/_vibewarden/health"},
		{"npm install", "npm install -g @vibewarden/cli"},
		{"install.sh fallback", "curl -fsSL https://vibewarden.dev/install.sh | sh"},
	}

	for _, r := range required {
		t.Run(r.name, func(t *testing.T) {
			if !strings.Contains(content, r.snippet) {
				t.Errorf("deploy template missing required command %q\n\nFull output:\n%s", r.snippet, content)
			}
		})
	}
}

// TestInstallStep_OffersBothInstallPaths guards Step 1 in both flavors
// (ADR-112). npm is the default path for the Node-equipped majority, and the
// shell installer must stay visible for everyone else: an agent on a machine
// without Node has no other way in. A future edit that drops either command
// silently breaks one of the two audiences, so both are asserted here.
func TestInstallStep_OffersBothInstallPaths(t *testing.T) {
	svc := newService(t)

	for _, deploy := range []bool{false, true} {
		name := "dev"
		if deploy {
			name = "deploy"
		}
		t.Run(name, func(t *testing.T) {
			out, err := svc.Render(fixtureOpts(deploy))
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			content := string(out)

			for _, want := range []string{
				"npm install -g @vibewarden/cli",
				"curl -fsSL https://vibewarden.dev/install.sh | sh",
			} {
				if !strings.Contains(content, want) {
					t.Errorf("%s flavor Step 1 is missing %q\n\nOutput:\n%s", name, want, content)
				}
			}
		})
	}
}

// TestNoDeployShInAnyFlavor is the forensic no-regression guard from the
// qr-code-blackhole retro (ADR-099 §Context). Neither flavor must contain
// the string "deploy.sh" — that file was removed in #1138 / ADR-088.
func TestNoDeployShInAnyFlavor(t *testing.T) {
	svc := newService(t)

	for _, deploy := range []bool{false, true} {
		name := "dev"
		if deploy {
			name = "deploy"
		}
		t.Run(name, func(t *testing.T) {
			out, err := svc.Render(fixtureOpts(deploy))
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			if strings.Contains(string(out), "deploy.sh") {
				t.Errorf("%s flavor output contains forbidden string \"deploy.sh\"\n\nOutput:\n%s", name, out)
			}
		})
	}
}
