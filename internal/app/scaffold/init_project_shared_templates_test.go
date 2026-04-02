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
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
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

// TestInitProject_UsesSharedArchitectTemplate verifies that architect.md is
// rendered from the language-agnostic agents/ template, not from go/.
// This ensures that adding a new language pack does not require duplicating the
// architect.md template.
func TestInitProject_UsesSharedArchitectTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	if !containsTemplate(renderer.renderToFileCalls, "agents/architect.md.tmpl") {
		t.Errorf("expected agents/architect.md.tmpl to be used; RenderToFile calls: %v", renderer.renderToFileCalls)
	}
	if containsTemplate(renderer.renderToFileCalls, "go/architect.md.tmpl") {
		t.Errorf("go/architect.md.tmpl must not be used; architect.md is a shared template")
	}
}

// TestInitProject_UsesSharedReviewerTemplate verifies that reviewer.md is
// rendered from the language-agnostic agents/ template, not from go/.
func TestInitProject_UsesSharedReviewerTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	if !containsTemplate(renderer.renderToFileCalls, "agents/reviewer.md.tmpl") {
		t.Errorf("expected agents/reviewer.md.tmpl to be used; RenderToFile calls: %v", renderer.renderToFileCalls)
	}
	if containsTemplate(renderer.renderToFileCalls, "go/reviewer.md.tmpl") {
		t.Errorf("go/reviewer.md.tmpl must not be used; reviewer.md is a shared template")
	}
}

// TestInitProject_UsesGoDevTemplate verifies that dev.md is rendered from the
// Go-language-specific go/ template.
func TestInitProject_UsesGoDevTemplate(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	if !containsTemplate(renderer.renderToFileCalls, "go/dev.md.tmpl") {
		t.Errorf("expected go/dev.md.tmpl to be used; RenderToFile calls: %v", renderer.renderToFileCalls)
	}
}

// TestInitProject_CLAUDEmd_UsesBothSharedAndGoTemplates verifies that CLAUDE.md
// is produced by combining the shared agents/claude.md.tmpl (vibew CLI reference,
// sidecar context) with the Go-specific go/claude.md.tmpl (code conventions).
func TestInitProject_CLAUDEmd_UsesBothSharedAndGoTemplates(t *testing.T) {
	renderer := newTrackingRenderer()
	svc := scaffoldapp.NewInitProjectService(renderer)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "myapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	// Both templates must be rendered via the Render (not RenderToFile) path
	// so their outputs can be concatenated into a single CLAUDE.md.
	if !containsTemplate(renderer.renderCalls, "agents/claude.md.tmpl") {
		t.Errorf("expected agents/claude.md.tmpl to be rendered; Render calls: %v", renderer.renderCalls)
	}
	if !containsTemplate(renderer.renderCalls, "go/claude.md.tmpl") {
		t.Errorf("expected go/claude.md.tmpl to be rendered; Render calls: %v", renderer.renderCalls)
	}

	// The output CLAUDE.md must exist and contain content from both renders.
	claudePath := filepath.Join(parent, "myapp", "CLAUDE.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("CLAUDE.md not found: %v", err)
	}
	content := string(data)

	// fakeRenderer output is "rendered:<templateName>" — verify both parts appear.
	if !strings.Contains(content, "rendered:agents/claude.md.tmpl") {
		t.Errorf("CLAUDE.md missing shared base content; got:\n%s", content)
	}
	if !strings.Contains(content, "rendered:go/claude.md.tmpl") {
		t.Errorf("CLAUDE.md missing Go conventions content; got:\n%s", content)
	}
}

// TestInitProject_SharedTemplatesWithRealFS verifies that the shared agent
// templates (architect.md, reviewer.md, claude.md) render correctly with the
// real embedded FS, producing output that contains the required vibew CLI
// reference and sidecar boundary rules.
func TestInitProject_SharedTemplatesWithRealFS(t *testing.T) {
	templateadapter := mustBuildRealRenderer(t)
	svc := scaffoldapp.NewInitProjectService(templateadapter)

	parent := t.TempDir()
	opts := scaffoldapp.InitProjectOptions{
		ProjectName: "realapp",
		Language:    domainscaffold.LanguageGo,
		Port:        3000,
	}

	if err := svc.InitProject(context.Background(), parent, opts); err != nil {
		t.Fatalf("InitProject() unexpected error: %v", err)
	}

	tests := []struct {
		file           string
		mustContain    []string
		mustNotContain []string
	}{
		{
			file: filepath.Join(parent, "realapp", ".claude", "agents", "architect.md"),
			mustContain: []string{
				"Sidecar boundary rule",
				"vibew CLI Reference",
				"./vibew dev",
				"./vibew doctor",
				"./vibew token",
				"./vibew secret get",
				"TLS termination",
				"App code focuses on",
			},
		},
		{
			file: filepath.Join(parent, "realapp", ".claude", "agents", "reviewer.md"),
			mustContain: []string{
				"Sidecar reimplementation rule",
				"REJECT",
				"vibew CLI Reference",
				"./vibew dev",
				"./vibew doctor",
				"Custom rate limiter",
				"Custom auth middleware",
				"TLS configuration in app code",
			},
		},
		{
			file: filepath.Join(parent, "realapp", "CLAUDE.md"),
			mustContain: []string{
				"vibew CLI Reference",
				"./vibew dev",
				"./vibew token",
				"./vibew secret get",
				"VibeWarden",
				// Go-specific conventions appended by go/claude.md.tmpl:
				"Code conventions",
				"gofmt",
			},
		},
		{
			file: filepath.Join(parent, "realapp", ".claude", "agents", "dev.md"),
			mustContain: []string{
				"vibew CLI Reference",
				"./vibew token",
				"./vibew doctor",
				// Go-specific idioms:
				"Go idioms",
				"gofmt",
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
			for _, absent := range tt.mustNotContain {
				if strings.Contains(content, absent) {
					t.Errorf("file %s unexpectedly contains %q\nContent:\n%s", tt.file, absent, content)
				}
			}
		})
	}
}
