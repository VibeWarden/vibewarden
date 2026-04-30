package promptkickoff_test

// release_artifact_test.go — forensic CI test for ADR-101.
//
// This test verifies that the rendered-and-rewritten output that the release
// wrapper script (scripts/release/emit-kickoff-artifacts.sh) uploads as
// GitHub Release assets satisfies the post-#1138 and post-#1217 deploy
// contracts and does NOT contain any of the known-bad forms (bash deploy.sh,
// scp glob, etc.).
//
// The test uses the same in-process Render path as the wrapper script, then
// applies the same post-render sed rewrite so no built binary is required.
// The wrapper script is exercised end-to-end by wrapper_script_test.go.

import (
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/promptkickoff"
	clitemplates "github.com/vibewarden/vibewarden/internal/cli/templates"
)

// sentinelRewrite applies the same substitution the release wrapper script
// applies via sed, converting internal sentinels to the public two-brace
// placeholder form.
func sentinelRewrite(s string) string {
	s = strings.ReplaceAll(s, "vwprjname", "{{prjname}}")
	s = strings.ReplaceAll(s, "vwdomain.example.invalid", "{{domain}}")
	// {{description}} is passed literally to --describe and survives
	// unsanitised through Render, so no rewrite is needed for that field.
	return s
}

// renderWithSentinels invokes Render with the three sentinel/literal values
// the release wrapper script passes and returns the post-rewrite body.
func renderWithSentinels(t *testing.T, deploy bool) string {
	t.Helper()
	renderer := templateadapter.NewRenderer(clitemplates.FS)
	svc := promptkickoff.NewService(renderer)

	opts := promptkickoff.Options{
		Name:         "vwprjname",
		Describe:     "{{description}}",
		Domain:       "vwdomain.example.invalid",
		VibewVersion: "v0.0.0-test",
		Deploy:       deploy,
	}
	if !deploy {
		// dev flavor: domain not required. Pass it anyway so the template
		// renders the TLS step with the domain sentinel.
		opts.Domain = "vwdomain.example.invalid"
	}

	raw, err := svc.Render(opts)
	if err != nil {
		t.Fatalf("Render(deploy=%v) error: %v", deploy, err)
	}
	return sentinelRewrite(string(raw))
}

// TestReleaseArtifact_PlaceholdersPresent asserts that all two-brace
// placeholders survive the sentinel rewrite and appear literally in both
// artifact bodies.
func TestReleaseArtifact_PlaceholdersPresent(t *testing.T) {
	tests := []struct {
		name   string
		deploy bool
		extra  []string
	}{
		{
			name:   "dev",
			deploy: false,
		},
		{
			name:   "deploy",
			deploy: true,
			extra:  []string{"{{domain}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderWithSentinels(t, tt.deploy)

			for _, ph := range []string{"{{prjname}}", "{{description}}"} {
				if !strings.Contains(body, ph) {
					t.Errorf("%s artifact: missing placeholder %q", tt.name, ph)
				}
			}
			for _, ph := range tt.extra {
				if !strings.Contains(body, ph) {
					t.Errorf("%s artifact: missing placeholder %q", tt.name, ph)
				}
			}
		})
	}
}

// TestReleaseArtifact_SentinelNotInOutput asserts that neither the name
// sentinel ("vwprjname") nor the domain sentinel ("vwdomain.example.invalid")
// survives the rewrite.
func TestReleaseArtifact_SentinelNotInOutput(t *testing.T) {
	for _, deploy := range []bool{false, true} {
		flavor := "dev"
		if deploy {
			flavor = "deploy"
		}
		t.Run(flavor, func(t *testing.T) {
			body := renderWithSentinels(t, deploy)

			if strings.Contains(body, "vwprjname") {
				t.Errorf("%s artifact: sentinel 'vwprjname' leaked into output", flavor)
			}
			if strings.Contains(body, "vwdomain.example.invalid") {
				t.Errorf("%s artifact: sentinel 'vwdomain.example.invalid' leaked into output", flavor)
			}
		})
	}
}

// TestReleaseArtifact_OptionalMkdirAnnotation asserts that the optional-mkdir
// annotation (ADR-101 §Annotation: optional mkdir) is present in both flavors.
func TestReleaseArtifact_OptionalMkdirAnnotation(t *testing.T) {
	for _, deploy := range []bool{false, true} {
		flavor := "dev"
		if deploy {
			flavor = "deploy"
		}
		t.Run(flavor, func(t *testing.T) {
			body := renderWithSentinels(t, deploy)
			if !strings.Contains(body, "# Skip if you're already in the project directory:") {
				t.Errorf("%s artifact: missing optional-mkdir annotation", flavor)
			}
		})
	}
}

// TestReleaseArtifact_DeployContract asserts that the deploy artifact contains
// the post-#1138 deploy contract and the post-#1217 dotfile-safe tar pipe
// transfer, and does NOT contain any of the known-bad forms.
func TestReleaseArtifact_DeployContract(t *testing.T) {
	body := renderWithSentinels(t, true)

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
		t.Run("contains/"+r.name, func(t *testing.T) {
			if !strings.Contains(body, r.snippet) {
				t.Errorf("deploy artifact missing required snippet %q (post-deploy-contract drift)\n\nFull output:\n%s",
					r.snippet, body)
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
		t.Run("absent/"+r.name, func(t *testing.T) {
			if strings.Contains(body, r.snippet) {
				t.Errorf("deploy artifact contains forbidden snippet %q (regression)\n\nFull output:\n%s",
					r.snippet, body)
			}
		})
	}
}

// TestReleaseArtifact_NoDeployShInDevFlavor mirrors the existing
// TestNoDeployShInAnyFlavor guard for the sentinel-based rendering path.
func TestReleaseArtifact_NoDeployShInDevFlavor(t *testing.T) {
	body := renderWithSentinels(t, false)
	if strings.Contains(body, "deploy.sh") {
		t.Errorf("dev artifact contains forbidden string 'deploy.sh'\n\nOutput:\n%s", body)
	}
}
