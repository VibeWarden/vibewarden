package bundle_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// renderFreshInitTemplates renders init-vibewarden.yaml.tmpl and
// init-vibewarden.production.yaml.tmpl into dir, mirroring what
// InitProjectService.InitProject does for a fresh project.
func renderFreshInitTemplates(t *testing.T, dir string) (basePath, prodPath string) {
	t.Helper()

	renderer := templateadapter.NewRenderer(templates.FS)
	data := domainscaffold.InitProjectData{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "myapp",
	}

	basePath = filepath.Join(dir, "vibewarden.yaml")
	prodPath = filepath.Join(dir, "vibewarden.production.yaml")

	rendered, err := renderer.Render("init-vibewarden.yaml.tmpl", data)
	if err != nil {
		t.Fatalf("rendering init-vibewarden.yaml.tmpl: %v", err)
	}
	if err := os.WriteFile(basePath, rendered, 0o600); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	renderedProd, err := renderer.Render("init-vibewarden.production.yaml.tmpl", data)
	if err != nil {
		t.Fatalf("rendering init-vibewarden.production.yaml.tmpl: %v", err)
	}
	if err := os.WriteFile(prodPath, renderedProd, 0o600); err != nil {
		t.Fatalf("writing vibewarden.production.yaml: %v", err)
	}

	return basePath, prodPath
}

// TestLoadMergedConfig_FreshInitTemplates_NoTLSError is the unit-level
// regression guard for #1178. It renders both init templates and calls
// LoadMergedConfig to confirm that the merged result passes validation without
// a tls.domain error — the root cause of the fresh-init bundle failure.
func TestLoadMergedConfig_FreshInitTemplates_NoTLSError(t *testing.T) {
	dir := t.TempDir()
	basePath, prodPath := renderFreshInitTemplates(t, dir)

	_, err := bundleapp.LoadMergedConfig(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadMergedConfig() on fresh init templates error = %v\n\nThis is the #1178 regression: the production template contained a stale tls.provider: letsencrypt block that fails validation when tls.domain is not set.", err)
	}
}

// TestBundle_FreshInitProject_Succeeds is the integration-level regression
// guard for #1178. It renders both init templates into a tempdir and runs the
// bundle pipeline with --skip-image semantics (SkipImage: true, no
// imageInspector wired), asserting that Bundle() returns nil and that the
// expected output artifacts are present.
func TestBundle_FreshInitProject_Succeeds(t *testing.T) {
	projDir := t.TempDir()
	basePath, prodPath := renderFreshInitTemplates(t, projDir)

	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myapp",
		MultiSite:      false,
		OutputDir:      outputDir,
		SkipImage:      true,
	})
	if err != nil {
		t.Fatalf("Bundle() on fresh init project error = %v\n\nThis is the #1178 regression: vibew bundle --skip-image must succeed on a fresh vibew init project.", err)
	}

	// vibewarden.yaml must exist in the bundle output directory.
	bundledConfig := filepath.Join(outputDir, "vibewarden.yaml")
	if _, statErr := os.Stat(bundledConfig); os.IsNotExist(statErr) {
		t.Error("expected vibewarden.yaml in bundle output directory")
	}
}

// TestBundle_FreshInitProject_TLSErrorHintsProductionYAML verifies that when
// a user manually adds tls.provider: letsencrypt to vibewarden.production.yaml
// without setting tls.domain, the resulting error message names the production
// file so the user knows where to look.
func TestBundle_FreshInitProject_TLSErrorHintsProductionYAML(t *testing.T) {
	projDir := t.TempDir()
	basePath, _ := renderFreshInitTemplates(t, projDir)

	// Manually write a production override that triggers the tls.domain error.
	brokenProd := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
`
	prodPath := filepath.Join(projDir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(brokenProd), 0o600); err != nil {
		t.Fatalf("writing broken prod config: %v", err)
	}

	generator := &fakeGenerator{}
	svc := bundleapp.NewService(&fakeExecutor{}, generator)

	outputDir := t.TempDir()

	err := svc.Bundle(context.Background(), bundleapp.BundleOptions{
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myapp",
		MultiSite:      false,
		OutputDir:      outputDir,
		SkipImage:      true,
	})
	if err == nil {
		t.Fatal("Bundle() expected error for tls.provider: letsencrypt without tls.domain, got nil")
	}

	// The error message must name vibewarden.production.yaml so the user knows
	// where to fix the misconfiguration.
	if !strings.Contains(err.Error(), "vibewarden.production.yaml") {
		t.Errorf("error message does not mention 'vibewarden.production.yaml'; got: %q\n\nExpected the bundle error wrapper to hint at the production file (architect note for #1178).", err.Error())
	}
}
