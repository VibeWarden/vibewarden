package scaffold_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			name:          "generic generates AGENTS-VIBEWARDEN.md and creates AGENTS.md",
			agentType:     domainscaffold.AgentTypeGeneric,
			opts:          baseOpts,
			wantPaths:     []string{"AGENTS-VIBEWARDEN.md", "AGENTS.md"},
			wantTemplates: []string{"agents.md.tmpl", "agents/agents.md.tmpl"},
		},
		{
			name:      "all generates two agent type file sets",
			agentType: domainscaffold.AgentTypeAll,
			opts:      baseOpts,
			wantPaths: []string{
				filepath.Join(".claude", "CLAUDE.md"),
				"AGENTS-VIBEWARDEN.md",
				"AGENTS.md",
			},
			wantTemplates: []string{"claude.md.tmpl", "agents.md.tmpl", "agents/agents.md.tmpl"},
		},
		{
			name:      "force false does not overwrite existing claude file",
			agentType: domainscaffold.AgentTypeClaude,
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

			// Pre-create .claude/CLAUDE.md to trigger the overwrite-false error case.
			if tt.name == "force false does not overwrite existing claude file" {
				claudePath := filepath.Join(dir, ".claude", "CLAUDE.md")
				if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(claudePath, []byte("existing"), 0o644); err != nil {
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

func TestAgentContextService_GenerateAgentContext_GenericAlwaysOverwrites(t *testing.T) {
	dir := t.TempDir()

	// Pre-create AGENTS-VIBEWARDEN.md with old content.
	vibewardenPath := filepath.Join(dir, "AGENTS-VIBEWARDEN.md")
	if err := os.WriteFile(vibewardenPath, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{
		UpstreamPort: 3000,
		Force:        false, // Force=false must NOT prevent AGENTS-VIBEWARDEN.md overwrite.
	}

	written, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeGeneric, opts)
	if err != nil {
		t.Fatalf("GenerateAgentContext() unexpected error: %v", err)
	}
	// Expect both AGENTS-VIBEWARDEN.md and AGENTS.md to be returned.
	const wantCount = 2
	if len(written) != wantCount {
		t.Fatalf("expected %d written paths, got %d", wantCount, len(written))
	}

	got, err := os.ReadFile(vibewardenPath)
	if err != nil {
		t.Fatalf("reading AGENTS-VIBEWARDEN.md: %v", err)
	}
	if string(got) == "old content" {
		t.Error("expected AGENTS-VIBEWARDEN.md to be overwritten, but old content remains")
	}
}

func TestAgentContextService_GenerateAgentContext_ExistingAGENTSMDGetsReference(t *testing.T) {
	dir := t.TempDir()

	// Pre-create AGENTS.md without any reference to AGENTS-VIBEWARDEN.md.
	agentsMDPath := filepath.Join(dir, "AGENTS.md")
	original := "# My project agent instructions\n\nDo stuff.\n"
	if err := os.WriteFile(agentsMDPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{UpstreamPort: 3000}
	_, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeGeneric, opts)
	if err != nil {
		t.Fatalf("GenerateAgentContext() unexpected error: %v", err)
	}

	got, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	content := string(got)

	// Original content must be preserved.
	if !strings.Contains(content, "My project agent instructions") {
		t.Error("existing AGENTS.md content was lost")
	}
	// Reference must have been appended.
	if !strings.Contains(content, "AGENTS-VIBEWARDEN.md") {
		t.Errorf("expected AGENTS.md to contain reference to AGENTS-VIBEWARDEN.md, got:\n%s", content)
	}
}

func TestAgentContextService_GenerateAgentContext_ExistingAGENTSMDWithReferenceUnchanged(t *testing.T) {
	dir := t.TempDir()

	// Pre-create AGENTS.md that already has the reference.
	agentsMDPath := filepath.Join(dir, "AGENTS.md")
	original := "# Instructions\n\nSee [AGENTS-VIBEWARDEN.md](./AGENTS-VIBEWARDEN.md) for VibeWarden sidecar instructions.\n"
	if err := os.WriteFile(agentsMDPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{UpstreamPort: 3000}
	_, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeGeneric, opts)
	if err != nil {
		t.Fatalf("GenerateAgentContext() unexpected error: %v", err)
	}

	got, err := os.ReadFile(agentsMDPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	// Content should be unchanged — reference not duplicated.
	if string(got) != original {
		t.Errorf("AGENTS.md was modified unnecessarily.\nWant:\n%s\nGot:\n%s", original, string(got))
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
	// Verify AgentTypeAll resolves to all individual types by calling
	// GenerateAgentContext and checking all expected files are created.
	dir := t.TempDir()
	renderer := newFakeAgentRenderer()
	svc := scaffoldapp.NewAgentContextService(renderer)

	opts := scaffoldapp.InitOptions{UpstreamPort: 3000}
	written, err := svc.GenerateAgentContext(context.Background(), dir, domainscaffold.AgentTypeAll, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// claude produces 1 file; generic produces 2 (AGENTS-VIBEWARDEN.md + AGENTS.md).
	const wantCount = 3
	if len(written) != wantCount {
		t.Errorf("AgentTypeAll produced %d files, want %d", len(written), wantCount)
	}
}
