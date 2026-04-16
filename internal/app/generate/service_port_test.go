package generate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestGenerate_ByteIdentical_AfterDTOMigration locks in byte-identical output
// between two equivalent Generate invocations after the ADR-064 migration:
//
//  1. the "canonical" path: cfg.ToGeneratorInput() built from a full config.
//  2. a "hand-built" path: a GeneratorInput constructed directly that preserves
//     TemplateData as the same *config.Config pointer.
//
// Both must produce byte-identical kratos.yml, identity.schema.json, and
// docker-compose.yml. This guards against future changes to the DTO shape
// accidentally diverging template output.
func TestGenerate_ByteIdentical_AfterDTOMigration(t *testing.T) {
	cfg := minimalConfig()

	// Path 1: canonical via ToGeneratorInput.
	dir1 := t.TempDir()
	out1 := filepath.Join(dir1, "generated")
	svc1 := generate.NewService(realRenderer())
	if err := svc1.Generate(context.Background(), cfg.ToGeneratorInput(), out1); err != nil {
		t.Fatalf("canonical Generate: %v", err)
	}

	// Path 2: hand-built input that still carries the same *config.Config as
	// TemplateData (this is what adapters must continue to do until the
	// Generate body is migrated off the type assertion).
	dir2 := t.TempDir()
	out2 := filepath.Join(dir2, "generated")
	svc2 := generate.NewService(realRenderer())
	handBuilt := ports.GeneratorInput{
		Profile:              cfg.Profile,
		AuthEnabled:          cfg.Auth.Enabled,
		AuthMode:             string(cfg.Auth.Mode),
		KratosExternal:       cfg.Kratos.External,
		SecretsEnabled:       cfg.Secrets.Enabled,
		ObservabilityEnabled: cfg.Observability.Enabled,
		TemplateData:         cfg,
	}
	if err := svc2.Generate(context.Background(), handBuilt, out2); err != nil {
		t.Fatalf("hand-built Generate: %v", err)
	}

	files := []string{
		filepath.Join("kratos", "kratos.yml"),
		filepath.Join("kratos", "identity.schema.json"),
		"docker-compose.yml",
	}
	for _, rel := range files {
		t.Run(rel, func(t *testing.T) {
			got1, err := os.ReadFile(filepath.Join(out1, rel))
			if err != nil {
				t.Fatalf("reading %s from canonical path: %v", rel, err)
			}
			got2, err := os.ReadFile(filepath.Join(out2, rel))
			if err != nil {
				t.Fatalf("reading %s from hand-built path: %v", rel, err)
			}
			if string(got1) != string(got2) {
				t.Errorf("%s diverged between DTO paths (len %d vs %d)", rel, len(got1), len(got2))
			}
		})
	}
}

// TestGenerate_WrongTemplateDataType_ReturnsError verifies that the Generate
// body's recovery cast rejects a non-*config.Config payload with a clear error,
// rather than panicking at render time. This is the sentinel guarding the
// "TemplateData is opaque to the port but the adapter knows what type it is"
// contract in ADR-064.
func TestGenerate_WrongTemplateDataType_ReturnsError(t *testing.T) {
	svc := generate.NewService(&fakeRenderer{})
	bad := ports.GeneratorInput{TemplateData: "not a config"}

	err := svc.Generate(context.Background(), bad, t.TempDir())
	if err == nil {
		t.Fatal("expected Generate with wrong TemplateData type to error, got nil")
	}
}
