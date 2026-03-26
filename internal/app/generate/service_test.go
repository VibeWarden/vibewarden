package generate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
)

// fakeRenderer is a minimal ports.TemplateRenderer that records calls and
// writes configurable content to the destination file.
type fakeRenderer struct {
	// renderFn overrides rendering behaviour when set.
	renderFn func(templateName string, data any) ([]byte, error)
}

func (f *fakeRenderer) Render(templateName string, data any) ([]byte, error) {
	if f.renderFn != nil {
		return f.renderFn(templateName, data)
	}
	return []byte("# rendered: " + templateName), nil
}

func (f *fakeRenderer) RenderToFile(templateName string, data any, path string, overwrite bool) error {
	rendered, err := f.Render(templateName, data)
	if err != nil {
		return fmt.Errorf("rendering %q: %w", templateName, err)
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file already exists at %q: %w", path, os.ErrExist)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("checking file %q: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	return os.WriteFile(path, rendered, 0o644)
}

func minimalConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Kratos: config.KratosConfig{
			PublicURL: "http://localhost:4433",
			AdminURL:  "http://localhost:4434",
			DSN:       "postgres://kratos:secret@localhost:5432/kratos?sslmode=disable",
			SMTP:      config.KratosSMTPConfig{Host: "localhost", Port: 1025, From: "no-reply@vibewarden.local"},
		},
		Auth: config.AuthConfig{
			Enabled:        true,
			IdentitySchema: "email_password",
		},
	}
}

func TestGenerate_CreatesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "generated")

	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	expectedFiles := []string{
		filepath.Join(outputDir, "kratos", "kratos.yml"),
		filepath.Join(outputDir, "kratos", "identity.schema.json"),
		filepath.Join(outputDir, "docker-compose.yml"),
	}

	for _, f := range expectedFiles {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, err := os.Stat(f); err != nil {
				t.Errorf("expected file %q to exist: %v", f, err)
			}
		})
	}
}

func TestGenerate_DefaultOutputDir(t *testing.T) {
	// Change working directory to a temp dir so default output dir is clean.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()

	if err := svc.Generate(context.Background(), cfg, ""); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	schemaPath := filepath.Join(tmpDir, ".vibewarden", "generated", "kratos", "identity.schema.json")
	if _, err := os.Stat(schemaPath); err != nil {
		t.Errorf("expected identity.schema.json to exist at default path: %v", err)
	}
}

func TestGenerate_IdentitySchemaPresets(t *testing.T) {
	tests := []struct {
		name           string
		identitySchema string
		wantSubstr     []byte
	}{
		{
			name:           "email_password preset",
			identitySchema: "email_password",
			wantSubstr:     []byte(`"email"`),
		},
		{
			name:           "email_only preset",
			identitySchema: "email_only",
			wantSubstr:     []byte(`"email"`),
		},
		{
			name:           "username_password preset",
			identitySchema: "username_password",
			wantSubstr:     []byte(`"username"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			outputDir := filepath.Join(dir, "generated")

			svc := generate.NewService(&fakeRenderer{})
			cfg := minimalConfig()
			cfg.Auth.IdentitySchema = tt.identitySchema

			if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			schemaPath := filepath.Join(outputDir, "kratos", "identity.schema.json")
			data, err := os.ReadFile(schemaPath)
			if err != nil {
				t.Fatalf("reading identity.schema.json: %v", err)
			}
			if !bytes.Contains(data, tt.wantSubstr) {
				t.Errorf("identity.schema.json does not contain %q", tt.wantSubstr)
			}
		})
	}
}

func TestGenerate_OverrideIdentitySchema(t *testing.T) {
	customSchema := `{"custom": true}`
	schemaDir := t.TempDir()
	customSchemaPath := filepath.Join(schemaDir, "custom.json")
	if err := os.WriteFile(customSchemaPath, []byte(customSchema), 0600); err != nil {
		t.Fatalf("writing custom schema: %v", err)
	}

	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	cfg.Overrides.IdentitySchema = customSchemaPath

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "kratos", "identity.schema.json"))
	if err != nil {
		t.Fatalf("reading identity.schema.json: %v", err)
	}
	if string(data) != customSchema {
		t.Errorf("identity.schema.json = %q, want %q", string(data), customSchema)
	}
}

func TestGenerate_OverrideKratosConfig(t *testing.T) {
	customKratos := "# custom kratos config"
	kratosDir := t.TempDir()
	customKratosPath := filepath.Join(kratosDir, "kratos.yml")
	if err := os.WriteFile(customKratosPath, []byte(customKratos), 0600); err != nil {
		t.Fatalf("writing custom kratos config: %v", err)
	}

	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	cfg.Overrides.KratosConfig = customKratosPath

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "kratos", "kratos.yml"))
	if err != nil {
		t.Fatalf("reading kratos.yml: %v", err)
	}
	if string(data) != customKratos {
		t.Errorf("kratos.yml = %q, want %q", string(data), customKratos)
	}
}

func TestGenerate_RendererError_PropagatesError(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{
		renderFn: func(_ string, _ any) ([]byte, error) {
			return nil, fmt.Errorf("render failed")
		},
	})
	cfg := minimalConfig()

	err := svc.Generate(context.Background(), cfg, outputDir)
	if err == nil {
		t.Error("Generate() expected error when renderer fails, got nil")
	}
}

func TestGenerate_UnknownPreset_ReturnsError(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	cfg.Auth.IdentitySchema = "no_such_preset_or_path_xyz"

	err := svc.Generate(context.Background(), cfg, outputDir)
	if err == nil {
		t.Error("Generate() expected error for unknown preset, got nil")
	}
}
