package templates_test

import (
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
