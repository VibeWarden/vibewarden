package templates_test

import (
	"regexp"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// TestAgentContextTemplates_AgentsVibewardenMD verifies that agents/agents-vibewarden.md.tmpl
// renders correctly with various feature flag combinations.
func TestAgentContextTemplates_AgentsVibewardenMD(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	tests := []struct {
		name         string
		data         domainscaffold.InitProjectData
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "basic render contains sidecar boundary rule",
			data: domainscaffold.InitProjectData{
				ProjectName: "myapp",
				Port:        3000,
			},
			wantContains: []string{
				"VibeWarden Sidecar",
				"Security boundary rule",
				"vibew dev",
				"/health",
			},
		},
		{
			name: "basic render contains known limitations section",
			data: domainscaffold.InitProjectData{
				ProjectName: "myapp",
				Port:        3000,
			},
			wantContains: []string{
				"Known limitations",
				"detect",
				"vibew doctor",
				"Multi-site",
				"vibew init",
				"vibew add tls --domain",
			},
			wantAbsent: []string{
				"vibew add waf` does not exist",
			},
		},
		{
			name: "with description includes description section",
			data: domainscaffold.InitProjectData{
				ProjectName: "myapp",
				Port:        3000,
				Description: "a payment processing service",
			},
			wantContains: []string{
				"a payment processing service",
			},
		},
		{
			name: "without description omits description section",
			data: domainscaffold.InitProjectData{
				ProjectName: "myapp",
				Port:        3000,
			},
			wantAbsent: []string{
				"Project description",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderer.Render("agents/agents-vibewarden.md.tmpl", tt.data)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			content := string(out)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf("agents-vibewarden.md.tmpl output missing %q\n\nContent:\n%s", want, content)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(content, absent) {
					t.Errorf("agents-vibewarden.md.tmpl output unexpectedly contains %q\n\nContent:\n%s", absent, content)
				}
			}
		})
	}
}

// TestAgentsVibewardenTemplate_NoDockerfileExamples verifies that the rendered
// agents-vibewarden.md.tmpl does not contain any Dockerfile code examples (FROM
// lines or fenced Dockerfile blocks) and does contain the required contract terms.
func TestAgentsVibewardenTemplate_NoDockerfileExamples(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	out, err := renderer.Render("agents/agents-vibewarden.md.tmpl", domainscaffold.InitProjectData{
		ProjectName: "testapp",
		Port:        3000,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := string(out)

	// MUST NOT contain a Dockerfile FROM line at the start of any line.
	fromLineRe := regexp.MustCompile(`(?m)^FROM `)
	if fromLineRe.MatchString(rendered) {
		t.Error("agents-vibewarden.md.tmpl must not contain a Dockerfile FROM line (^FROM )")
	}

	// MUST NOT contain fenced Dockerfile blocks (any case variant).
	for _, fence := range []string{"```Dockerfile", "```dockerfile", "```docker"} {
		if strings.Contains(rendered, fence) {
			t.Errorf("agents-vibewarden.md.tmpl must not contain fenced block %q", fence)
		}
	}

	// MUST contain the contract terms that prove the new section is present.
	mustContain := []string{
		"Alpine",
		"EXPOSE",
		"upstream.port",
		"/health",
		"No `HEALTHCHECK`",
	}
	for _, want := range mustContain {
		if !strings.Contains(rendered, want) {
			t.Errorf("agents-vibewarden.md.tmpl missing required contract term %q", want)
		}
	}
}

// TestAgentsVibewardenTemplate_EjectRowDisambiguated verifies that the rendered
// agents-vibewarden.md.tmpl eject row no longer says "Docker Compose" and does
// say "Caddy" — this locks the fix for the qr-dali agent confusion (ADR-096).
func TestAgentsVibewardenTemplate_EjectRowDisambiguated(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	out, err := renderer.Render("agents/agents-vibewarden.md.tmpl", domainscaffold.InitProjectData{
		ProjectName: "testapp",
		Port:        3000,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := string(out)

	ejectDockerCompose := regexp.MustCompile(`vibew eject.*Docker Compose`)
	if ejectDockerCompose.MatchString(rendered) {
		t.Error("agents-vibewarden.md.tmpl eject row must not mention 'Docker Compose' — update the template (ADR-096)")
	}

	ejectCaddy := regexp.MustCompile(`vibew eject.*Caddy`)
	if !ejectCaddy.MatchString(rendered) {
		t.Error("agents-vibewarden.md.tmpl eject row must mention 'Caddy' to disambiguate from vibew bundle (ADR-096)")
	}
}

// TestAgentContextTemplates_AgentsMd verifies that agents/agents.md.tmpl renders
// the expected reference to AGENTS-VIBEWARDEN.md.
func TestAgentContextTemplates_AgentsMd(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	out, err := renderer.Render("agents/agents.md.tmpl", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(out)

	if !strings.Contains(content, "AGENTS-VIBEWARDEN.md") {
		t.Errorf("agents/agents.md.tmpl must reference AGENTS-VIBEWARDEN.md\n\nContent:\n%s", content)
	}
}
