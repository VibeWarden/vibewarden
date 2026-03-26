package scaffold_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// fakeAgentRenderer records calls so tests can assert which templates were used.
type fakeAgentRenderer struct {
	// calls maps (templateName, path) -> rendered content written.
	calls map[string]string
	// errOnTemplate, when set, causes RenderToFile to fail for that template name.
	errOnTemplate string
}

func newFakeAgentRenderer() *fakeAgentRenderer {
	return &fakeAgentRenderer{calls: make(map[string]string)}
}

func (f *fakeAgentRenderer) Render(templateName string, _ any) ([]byte, error) {
	return []byte(fmt.Sprintf("rendered:%s", templateName)), nil
}

func (f *fakeAgentRenderer) RenderToFile(templateName string, _ any, path string, overwrite bool) error {
	if f.errOnTemplate != "" && f.errOnTemplate == templateName {
		return fmt.Errorf("injected render error for %s", templateName)
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file exists: %w", os.ErrExist)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("rendered:%s", templateName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	f.calls[templateName] = path
	return nil
}

func TestAgentContextService_GenerateAgentContext(t *testing.T) {
	baseOpts := scaffoldapp.InitOptions{
		UpstreamPort:     3000,
		AuthEnabled:      true,
		RateLimitEnabled: true,
	}

	tests := []struct {
		name          string
		agentType     domainscaffold.AgentType
		opts          scaffoldapp.InitOptions
		wantPaths     []string
		wantTemplates []string
		wantErr       bool
	}{
		{
			name:          "claude generates .claude/CLAUDE.md",
			agentType:     domainscaffold.AgentTypeClaude,
			opts:          baseOpts,
			wantPaths:     []string{filepath.Join(".claude", "CLAUDE.md")},
			wantTemplates: []string{"claude.md.tmpl"},
		},
		{
			name:          "cursor generates .cursor/rules",
			agentType:     domainscaffold.AgentTypeCursor,
			opts:          baseOpts,
			wantPaths:     []string{filepath.Join(".cursor", "rules")},
			wantTemplates: []string{"cursor-rules.tmpl"},
		},
		{
			name:          "generic generates AGENTS.md",
			agentType:     domainscaffold.AgentTypeGeneric,
			opts:          baseOpts,
			wantPaths:     []string{"AGENTS.md"},
			wantTemplates: []string{"agents.md.tmpl"},
		},
		{
			name:      "all generates three files",
			agentType: domainscaffold.AgentTypeAll,
			opts:      baseOpts,
			wantPaths: []string{
				filepath.Join(".claude", "CLAUDE.md"),
				filepath.Join(".cursor", "rules"),
				"AGENTS.md",
			},
			wantTemplates: []string{"claude.md.tmpl", "cursor-rules.tmpl", "agents.md.tmpl"},
		},
		{
			name:      "force false does not overwrite existing file",
			agentType: domainscaffold.AgentTypeGeneric,
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				Force:        false,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			renderer := newFakeAgentRenderer()

			// Pre-create AGENTS.md to trigger the overwrite-false error case.
			if tt.name == "force false does not overwrite existing file" {
				agentsPath := filepath.Join(dir, "AGENTS.md")
				if err := os.WriteFile(agentsPath, []byte("existing"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			svc := scaffoldapp.NewAgentContextService(renderer)
			written, err := svc.GenerateAgentContext(context.Background(), dir, tt.agentType, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateAgentContext() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if len(written) != len(tt.wantPaths) {
				t.Fatalf("GenerateAgentContext() returned %d paths, want %d", len(written), len(tt.wantPaths))
			}

			for _, relPath := range tt.wantPaths {
				absPath := filepath.Join(dir, relPath)
				if _, err := os.Stat(absPath); err != nil {
					t.Errorf("expected file %q to exist: %v", absPath, err)
				}
			}

			for _, tmplName := range tt.wantTemplates {
				if _, ok := renderer.calls[tmplName]; !ok {
					t.Errorf("expected template %q to be rendered, but it was not", tmplName)
				}
			}
		})
	}
}

func TestAgentContextService_GenerateAgentContext_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	// Pre-create AGENTS.md with old content.
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{
		UpstreamPort: 3000,
		Force:        true,
	}

	written, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeGeneric, opts)
	if err != nil {
		t.Fatalf("GenerateAgentContext() unexpected error: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 written path, got %d", len(written))
	}

	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if string(got) == "old content" {
		t.Error("expected AGENTS.md to be overwritten, but old content remains")
	}
}

func TestAgentContextService_GenerateAgentContext_RenderError(t *testing.T) {
	dir := t.TempDir()
	renderer := newFakeAgentRenderer()
	renderer.errOnTemplate = "claude.md.tmpl"

	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{UpstreamPort: 3000}
	_, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeClaude, opts)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestResolveAgentTypes_AllExpands(t *testing.T) {
	// Verify AgentTypeAll resolves to all three individual types by calling
	// GenerateAgentContext and checking all three files are created.
	dir := t.TempDir()
	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{UpstreamPort: 3000}
	written, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeAll, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const wantCount = 3
	if len(written) != wantCount {
		t.Errorf("AgentTypeAll produced %d files, want %d", len(written), wantCount)
	}
}
