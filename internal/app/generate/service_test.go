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
		{
			name:           "social preset",
			identitySchema: "social",
			wantSubstr:     []byte(`"picture"`),
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

func TestGenerate_OverrideComposeFile(t *testing.T) {
	customCompose := "# custom docker-compose config"
	composeDir := t.TempDir()
	customComposePath := filepath.Join(composeDir, "docker-compose.override.yml")
	if err := os.WriteFile(customComposePath, []byte(customCompose), 0600); err != nil {
		t.Fatalf("writing custom compose file: %v", err)
	}

	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	cfg.Overrides.ComposeFile = customComposePath

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading docker-compose.yml: %v", err)
	}
	if string(data) != customCompose {
		t.Errorf("docker-compose.yml = %q, want %q", string(data), customCompose)
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

func TestGenerate_WithSocialProviders_GeneratesMapperFiles(t *testing.T) {
	tests := []struct {
		name          string
		providers     []config.SocialProviderConfig
		wantMappers   []string
		wantNoMappers []string
	}{
		{
			name: "google provider uses google mapper",
			providers: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
			},
			wantMappers:   []string{"google.jsonnet"},
			wantNoMappers: []string{"github.jsonnet"},
		},
		{
			name: "github provider uses github mapper",
			providers: []config.SocialProviderConfig{
				{Provider: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
			},
			wantMappers:   []string{"github.jsonnet"},
			wantNoMappers: []string{"google.jsonnet"},
		},
		{
			name: "generic oidc provider uses generic mapper",
			providers: []config.SocialProviderConfig{
				{Provider: "oidc", ID: "acme", ClientID: "oid", ClientSecret: "osecret", IssuerURL: "https://accounts.acme.example"},
			},
			wantMappers:   []string{"generic.jsonnet"},
			wantNoMappers: []string{"google.jsonnet", "github.jsonnet"},
		},
		{
			name: "multiple providers with overlapping mapper type",
			providers: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
				{Provider: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
				{Provider: "gitlab", ClientID: "glid", ClientSecret: "glsecret"},
			},
			wantMappers:   []string{"google.jsonnet", "github.jsonnet", "generic.jsonnet"},
			wantNoMappers: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			svc := generate.NewService(&fakeRenderer{})
			cfg := minimalConfig()
			cfg.Auth.SocialProviders = tt.providers

			if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			mappersDir := filepath.Join(outputDir, "kratos", "mappers")

			for _, m := range tt.wantMappers {
				path := filepath.Join(mappersDir, m)
				if _, err := os.Stat(path); err != nil {
					t.Errorf("expected mapper file %q to exist: %v", path, err)
				}
			}

			for _, m := range tt.wantNoMappers {
				path := filepath.Join(mappersDir, m)
				if _, err := os.Stat(path); err == nil {
					t.Errorf("unexpected mapper file %q exists", path)
				}
			}
		})
	}
}

func TestGenerate_MapperFilesContainValidJsonnet(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "generated")

	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	cfg.Auth.SocialProviders = []config.SocialProviderConfig{
		{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
		{Provider: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
		{Provider: "oidc", ID: "custom", ClientID: "oid", ClientSecret: "osecret", IssuerURL: "https://issuer.example"},
	}

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	mappers := []string{"google.jsonnet", "github.jsonnet", "generic.jsonnet"}
	for _, m := range mappers {
		path := filepath.Join(outputDir, "kratos", "mappers", m)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading mapper %q: %v", m, err)
		}
		if !bytes.Contains(data, []byte("identity")) {
			t.Errorf("mapper %q does not contain 'identity' key, content: %s", m, data)
		}
		if !bytes.Contains(data, []byte("traits")) {
			t.Errorf("mapper %q does not contain 'traits' key, content: %s", m, data)
		}
	}
}

func TestGenerate_WithoutSocialProviders_NoMappersDir(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "generated")

	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()
	// No social providers configured.

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	mappersDir := filepath.Join(outputDir, "kratos", "mappers")
	if _, err := os.Stat(mappersDir); err == nil {
		t.Errorf("expected mappers directory NOT to exist when no social providers configured, but it does: %q", mappersDir)
	}
}

func TestGenerate_SocialProviders_AutoSelectsSocialSchema(t *testing.T) {
	tests := []struct {
		name             string
		identitySchema   string
		socialProviders  []config.SocialProviderConfig
		wantSchemaSubstr []byte
		wantNoSubstr     []byte
	}{
		{
			name:           "auto-upgrades email_password to social when providers configured",
			identitySchema: "email_password",
			socialProviders: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
			},
			wantSchemaSubstr: []byte(`"picture"`),
			wantNoSubstr:     nil,
		},
		{
			name:           "auto-upgrades when identity_schema is empty (uses default)",
			identitySchema: "",
			socialProviders: []config.SocialProviderConfig{
				{Provider: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
			},
			wantSchemaSubstr: []byte(`"picture"`),
			wantNoSubstr:     nil,
		},
		{
			name:           "does not upgrade when explicit non-default schema is set",
			identitySchema: "email_only",
			socialProviders: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
			},
			wantSchemaSubstr: []byte(`"email"`),
			wantNoSubstr:     []byte(`"picture"`),
		},
		{
			name:           "explicit social schema is used as-is",
			identitySchema: "social",
			socialProviders: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "gid", ClientSecret: "gsecret"},
			},
			wantSchemaSubstr: []byte(`"picture"`),
			wantNoSubstr:     nil,
		},
		{
			name:             "no social providers keeps email_password schema",
			identitySchema:   "email_password",
			socialProviders:  nil,
			wantSchemaSubstr: []byte(`"email"`),
			wantNoSubstr:     []byte(`"picture"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			svc := generate.NewService(&fakeRenderer{})
			cfg := minimalConfig()
			cfg.Auth.IdentitySchema = tt.identitySchema
			cfg.Auth.SocialProviders = tt.socialProviders

			if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			schemaPath := filepath.Join(outputDir, "kratos", "identity.schema.json")
			data, err := os.ReadFile(schemaPath)
			if err != nil {
				t.Fatalf("reading identity.schema.json: %v", err)
			}
			if !bytes.Contains(data, tt.wantSchemaSubstr) {
				t.Errorf("identity.schema.json does not contain %q; content: %s", tt.wantSchemaSubstr, data)
			}
			if tt.wantNoSubstr != nil && bytes.Contains(data, tt.wantNoSubstr) {
				t.Errorf("identity.schema.json unexpectedly contains %q; content: %s", tt.wantNoSubstr, data)
			}
		})
	}
}

func TestGenerate_WithSocialProviders_KratosTemplatePassesConfig(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "generated")

	var capturedData any
	renderer := &fakeRenderer{
		renderFn: func(templateName string, data any) ([]byte, error) {
			if templateName == "kratos.yml.tmpl" {
				capturedData = data
			}
			return []byte("# rendered: " + templateName), nil
		},
	}

	svc := generate.NewService(renderer)
	cfg := minimalConfig()
	cfg.Auth.SocialProviders = []config.SocialProviderConfig{
		{Provider: "google", ClientID: "my-client-id", ClientSecret: "my-secret"},
	}

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	if capturedData == nil {
		t.Fatal("kratos.yml.tmpl was not rendered")
	}

	renderedCfg, ok := capturedData.(*config.Config)
	if !ok {
		t.Fatalf("expected *config.Config passed to renderer, got %T", capturedData)
	}

	if len(renderedCfg.Auth.SocialProviders) != 1 {
		t.Errorf("expected 1 social provider in rendered config, got %d", len(renderedCfg.Auth.SocialProviders))
	}
	if renderedCfg.Auth.SocialProviders[0].ClientID != "my-client-id" {
		t.Errorf("expected ClientID %q, got %q", "my-client-id", renderedCfg.Auth.SocialProviders[0].ClientID)
	}
}
