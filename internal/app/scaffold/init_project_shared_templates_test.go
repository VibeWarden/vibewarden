package scaffold_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// mustBuildRealRenderer returns a TemplateRenderer backed by the real embedded
// templates.FS. This is used by tests that verify actual template content rather
// than just file existence.
func mustBuildRealRenderer(t *testing.T) ports.TemplateRenderer {
	t.Helper()
	return templateadapter.NewRenderer(templates.FS)
}

// trackingRenderer wraps fakeRenderer and records which template names were
// rendered via RenderToFile and which were rendered via Render.
type trackingRenderer struct {
	*fakeRenderer
	renderCalls       []string
	renderToFileCalls []string
}

func newTrackingRenderer() *trackingRenderer {
	return &trackingRenderer{fakeRenderer: newFakeRenderer()}
}

func (t *trackingRenderer) Render(templateName string, data any) ([]byte, error) {
	t.renderCalls = append(t.renderCalls, templateName)
	return t.fakeRenderer.Render(templateName, data)
}

func (t *trackingRenderer) RenderToFile(templateName string, data any, path string, overwrite bool) error {
	t.renderToFileCalls = append(t.renderToFileCalls, templateName)
	return t.fakeRenderer.RenderToFile(templateName, data, path, overwrite)
}

// containsTemplate reports whether templateName appears in calls.
func containsTemplate(calls []string, templateName string) bool {
	for _, c := range calls {
		if c == templateName {
			return true
		}
	}
	return false
}

// TestInitProject_UsesAgentsVibewardenTemplate verifies that AGENTS-VIBEWARDEN.md
// is rendered from the shared agents/agents-vibewarden.md.tmpl template.
func TestInitProject_UsesAgentsVibewardenTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	if !containsTemplate(renderer.renderCalls, "agents/agents-vibewarden.md.tmpl") {
		t.Errorf("expected agents/agents-vibewarden.md.tmpl to be used; Render calls: %v", renderer.renderCalls)
	}
}

// TestInitProject_UsesAgentsMDTemplate verifies that agents/agents.md.tmpl is used
// when creating a new AGENTS.md.
func TestInitProject_UsesAgentsMDTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	if !containsTemplate(renderer.renderToFileCalls, "agents/agents.md.tmpl") {
		t.Errorf("expected agents/agents.md.tmpl to be used; RenderToFile calls: %v", renderer.renderToFileCalls)
	}
}

// TestInitProject_NoCLAUDEmdTemplate verifies that no claude.md.tmpl is used.
func TestInitProject_NoCLAUDEmdTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	for _, call := range renderer.renderCalls {
		if strings.Contains(call, "claude.md.tmpl") {
			t.Errorf("claude.md.tmpl must not be rendered by vibew init; got call: %q", call)
		}
	}
	for _, call := range renderer.renderToFileCalls {
		if strings.Contains(call, "claude.md.tmpl") {
			t.Errorf("claude.md.tmpl must not be used by vibew init; got call: %q", call)
		}
	}
}

// TestInitProject_SharedTemplatesWithRealFS verifies that AGENTS-VIBEWARDEN.md
// renders correctly with the real embedded FS.
func TestInitProject_SharedTemplatesWithRealFS(t *testing.T) {
	templateadapter := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(templateadapter, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "realapp",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	tests := []struct {
		file        string
		mustContain []string
	}{
		{
			file: filepath.Join(parent, "realapp", "AGENTS-VIBEWARDEN.md"),
			mustContain: []string{
				"VibeWarden Sidecar",
				"Security boundary rule",
				"vibew CLI Reference",
				"vibew dev",
				"vibew doctor",
				"vibew token",
				"vibew secret get/set/list",
				"TLS termination",
				"App code focuses on",
			},
		},
		{
			file: filepath.Join(parent, "realapp", "AGENTS.md"),
			mustContain: []string{
				"AGENTS-VIBEWARDEN.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			content := string(data)
			for _, want := range tt.mustContain {
				if !strings.Contains(content, want) {
					t.Errorf("file %s missing %q\nContent:\n%s", tt.file, want, content)
				}
			}
		})
	}
}

// TestInitProject_WithRealFS_Description verifies that the real templates render
// the description into PROJECT.md and AGENTS-VIBEWARDEN.md.
func TestInitProject_WithRealFS_Description(t *testing.T) {
	r := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(r, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "realwithDesc",
		Port:        3000,
		Description: "a payment processing service",
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	tests := []struct {
		file        string
		mustContain []string
	}{
		{
			file:        filepath.Join(parent, "realwithDesc", "PROJECT.md"),
			mustContain: []string{"a payment processing service", "realwithDesc"},
		},
		{
			file:        filepath.Join(parent, "realwithDesc", "AGENTS-VIBEWARDEN.md"),
			mustContain: []string{"a payment processing service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			content := string(data)
			for _, want := range tt.mustContain {
				if !strings.Contains(content, want) {
					t.Errorf("file %s missing %q\nContent:\n%s", tt.file, want, content)
				}
			}
		})
	}
}

// TestInitProject_ProductionYAML_HasDeployTargetPlatform verifies that vibew init
// writes deploy.target_platform: linux/amd64 as an actual yaml entry (not a
// comment) in vibewarden.production.yaml. Guard for #1200.
func TestInitProject_ProductionYAML_HasDeployTargetPlatform(t *testing.T) {
	r := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(r, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "deploytest",
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	prodPath := filepath.Join(parent, "deploytest", "vibewarden.production.yaml")
	data, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("reading vibewarden.production.yaml: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "target_platform: linux/amd64") {
		t.Errorf("vibewarden.production.yaml missing 'target_platform: linux/amd64'\n\nContent:\n%s", content)
	}

	// Verify it is not commented out.
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "target_platform: linux/amd64") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				t.Errorf("target_platform is commented out — must be active yaml\nLine: %q", line)
			}
		}
	}

	if !strings.Contains(content, "deploy:") {
		t.Errorf("vibewarden.production.yaml missing 'deploy:' block\n\nContent:\n%s", content)
	}
}

// TestInitProject_NoNameFlag_DefaultsToDirname verifies that when opts.Name is
// empty, vibewarden.yaml contains name: <ProjectName> (the directory basename).
// Guard for #1199: init didn't populate name: without --name, causing
// ComposeProjectName() to fall through to different values per environment.
func TestInitProject_NoNameFlag_DefaultsToDirname(t *testing.T) {
	r := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(r, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
		// Name intentionally omitted — must default to ProjectName.
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(parent, "myapp", "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `name: "myapp"`) {
		t.Errorf("vibewarden.yaml missing 'name: \"myapp\"' (dirname default)\n\nContent:\n%s", content)
	}
}

// TestInitProject_ExplicitName_OverridesDirname verifies that when opts.Name is
// set explicitly, vibewarden.yaml contains that name, not the dirname.
func TestInitProject_ExplicitName_OverridesDirname(t *testing.T) {
	r := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(r, nil)

	parent := scaffoldAppTestDir(t)
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "custom-name",
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(parent, "myapp", "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading vibewarden.yaml: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `name: "custom-name"`) {
		t.Errorf("vibewarden.yaml missing 'name: \"custom-name\"'\n\nContent:\n%s", content)
	}
}
