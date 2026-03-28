package generate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
	gendomain "github.com/vibewarden/vibewarden/internal/domain/generate"
)

// realRenderer returns a TemplateRenderer backed by the embedded config templates FS.
// Use this for tests that need to verify actual template output.
func realRenderer() *templateadapter.Renderer {
	return templateadapter.NewRenderer(configtemplates.FS)
}

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

// appServiceConfig returns a Config with the app section set according to the
// supplied build and image values. The auth section is kept disabled so the
// tests focus on the app service in isolation.
func appServiceConfig(build, image string) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		App:      config.AppConfig{Build: build, Image: image},
	}
}

// renderCompose runs Generate with the real template renderer and returns the
// contents of the generated docker-compose.yml as a byte slice.
func renderCompose(t *testing.T, cfg *config.Config) []byte {
	t.Helper()
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading docker-compose.yml: %v", err)
	}
	return data
}

func TestGenerate_AppService_BuildMode(t *testing.T) {
	cfg := appServiceConfig(".", "")
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("app:")) {
		t.Error("expected 'app:' service to be present")
	}
	if !bytes.Contains(compose, []byte("build:")) {
		t.Error("expected 'build:' directive")
	}
	if !bytes.Contains(compose, []byte("context: .")) {
		t.Error("expected 'context: .'")
	}
	if bytes.Contains(compose, []byte("image: ${VIBEWARDEN_APP_IMAGE")) {
		t.Error("image: directive must not appear in build mode")
	}
}

func TestGenerate_AppService_ImageMode(t *testing.T) {
	cfg := appServiceConfig("", "ghcr.io/org/myapp:latest")
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("app:")) {
		t.Error("expected 'app:' service to be present")
	}
	if !bytes.Contains(compose, []byte("image: ${VIBEWARDEN_APP_IMAGE:-ghcr.io/org/myapp:latest}")) {
		t.Error("expected image with VIBEWARDEN_APP_IMAGE env var override")
	}
	if bytes.Contains(compose, []byte("build:")) {
		t.Error("build: directive must not appear in image mode")
	}
}

func TestGenerate_AppService_BothSet_BuildTakesPrecedence(t *testing.T) {
	cfg := appServiceConfig("./src", "ghcr.io/org/myapp:latest")
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("build:")) {
		t.Error("expected 'build:' directive when both build and image are set")
	}
	if !bytes.Contains(compose, []byte("context: ./src")) {
		t.Error("expected 'context: ./src'")
	}
	// image: for the app service must not appear when build takes precedence.
	// Note: "image:" still appears in vibewarden service itself so we check
	// for the VIBEWARDEN_APP_IMAGE override pattern specifically.
	if bytes.Contains(compose, []byte("image: ${VIBEWARDEN_APP_IMAGE")) {
		t.Error("image: env-override directive must not appear when build takes precedence")
	}
}

func TestGenerate_AppService_NeitherSet_NoAppService(t *testing.T) {
	cfg := appServiceConfig("", "")
	compose := renderCompose(t, cfg)

	if bytes.Contains(compose, []byte("\n  app:")) {
		t.Error("app service must not be rendered when neither build nor image is set")
	}
}

func TestGenerate_AppService_DependsOn(t *testing.T) {
	tests := []struct {
		name      string
		build     string
		image     string
		wantDepOn bool
	}{
		{"build mode has depends_on app", ".", "", true},
		{"image mode has depends_on app", "", "ghcr.io/org/myapp:latest", true},
		{"no app service has no depends_on app", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := appServiceConfig(tt.build, tt.image)
			compose := renderCompose(t, cfg)

			hasDepOn := bytes.Contains(compose, []byte("app:\n        condition: service_healthy"))
			if hasDepOn != tt.wantDepOn {
				t.Errorf("depends_on app present=%v, want %v\ncompose:\n%s", hasDepOn, tt.wantDepOn, compose)
			}
		})
	}
}

func TestGenerate_AppService_UpstreamHost(t *testing.T) {
	tests := []struct {
		name             string
		build            string
		image            string
		wantUpstreamHost string
		wantExtraHosts   bool
	}{
		{
			name:             "build mode uses app container name",
			build:            ".",
			image:            "",
			wantUpstreamHost: "VIBEWARDEN_UPSTREAM_HOST=app",
			wantExtraHosts:   false,
		},
		{
			name:             "image mode uses app container name",
			build:            "",
			image:            "ghcr.io/org/myapp:latest",
			wantUpstreamHost: "VIBEWARDEN_UPSTREAM_HOST=app",
			wantExtraHosts:   false,
		},
		{
			name:             "no app service falls back to host.docker.internal",
			build:            "",
			image:            "",
			wantUpstreamHost: "VIBEWARDEN_UPSTREAM_HOST=host.docker.internal",
			wantExtraHosts:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := appServiceConfig(tt.build, tt.image)
			compose := renderCompose(t, cfg)

			if !bytes.Contains(compose, []byte(tt.wantUpstreamHost)) {
				t.Errorf("expected %q in compose output\ncompose:\n%s", tt.wantUpstreamHost, compose)
			}
			hasExtraHosts := bytes.Contains(compose, []byte("extra_hosts:"))
			if hasExtraHosts != tt.wantExtraHosts {
				t.Errorf("extra_hosts present=%v, want %v", hasExtraHosts, tt.wantExtraHosts)
			}
		})
	}
}

// secretsConfig returns a Config with the secrets section configured as requested.
func secretsConfig(enabled bool, headers []config.SecretsHeaderInjection, env []config.SecretsEnvInjection) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Secrets: config.SecretsConfig{
			Enabled: enabled,
			OpenBao: config.SecretsOpenBaoConfig{
				MountPath: "secret",
			},
			Inject: config.SecretsInjectConfig{
				Headers: headers,
				Env:     env,
			},
		},
	}
}

func TestGenerate_OpenBaoService_SecretEnabled(t *testing.T) {
	cfg := secretsConfig(true, nil, nil)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("openbao:")) {
		t.Error("expected 'openbao:' service when secrets.enabled is true")
	}
	if !bytes.Contains(compose, []byte("quay.io/openbao/openbao:")) {
		t.Error("expected openbao image reference")
	}
	if !bytes.Contains(compose, []byte("BAO_DEV_ROOT_TOKEN_ID:")) {
		t.Error("expected BAO_DEV_ROOT_TOKEN_ID env var")
	}
	if !bytes.Contains(compose, []byte("VIBEWARDEN_SECRETS_OPENBAO_ADDRESS=http://openbao:8200")) {
		t.Error("expected VIBEWARDEN_SECRETS_OPENBAO_ADDRESS env var in vibewarden service")
	}
	if !bytes.Contains(compose, []byte("VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN=")) {
		t.Error("expected VIBEWARDEN_SECRETS_OPENBAO_AUTH_TOKEN env var in vibewarden service")
	}
}

func TestGenerate_OpenBaoService_SecretDisabled(t *testing.T) {
	cfg := secretsConfig(false, nil, nil)
	compose := renderCompose(t, cfg)

	if bytes.Contains(compose, []byte("openbao:")) {
		t.Error("openbao service must not be present when secrets.enabled is false")
	}
	if bytes.Contains(compose, []byte("VIBEWARDEN_SECRETS_OPENBAO_ADDRESS")) {
		t.Error("VIBEWARDEN_SECRETS_OPENBAO_ADDRESS must not be present when secrets.enabled is false")
	}
}

func TestGenerate_SeedSecretsService_WithInjectHeaders(t *testing.T) {
	headers := []config.SecretsHeaderInjection{
		{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
	}
	cfg := secretsConfig(true, headers, nil)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("seed-secrets:")) {
		t.Error("expected 'seed-secrets:' service when secrets.enabled and inject.headers are configured")
	}
	if !bytes.Contains(compose, []byte("seed-secrets.sh")) {
		t.Error("expected seed-secrets.sh volume mount in seed-secrets service")
	}
	if !bytes.Contains(compose, []byte("service_completed_successfully")) {
		t.Error("expected vibewarden depends_on seed-secrets with condition service_completed_successfully")
	}
}

func TestGenerate_SeedSecretsService_WithInjectEnv(t *testing.T) {
	env := []config.SecretsEnvInjection{
		{SecretPath: "app/db-pass", SecretKey: "password", EnvVar: "DB_PASSWORD"},
	}
	cfg := secretsConfig(true, nil, env)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("seed-secrets:")) {
		t.Error("expected 'seed-secrets:' service when secrets.enabled and inject.env are configured")
	}
	if !bytes.Contains(compose, []byte("service_completed_successfully")) {
		t.Error("expected vibewarden depends_on seed-secrets with condition service_completed_successfully")
	}
}

func TestGenerate_SeedSecretsService_NoInject_NoSeedContainer(t *testing.T) {
	cfg := secretsConfig(true, nil, nil)
	compose := renderCompose(t, cfg)

	if bytes.Contains(compose, []byte("seed-secrets:")) {
		t.Error("seed-secrets service must not be present when inject is empty")
	}
	// openbao should be directly depended upon instead
	if !bytes.Contains(compose, []byte("openbao:\n        condition: service_healthy")) {
		t.Errorf("expected vibewarden depends_on openbao with service_healthy when no inject entries\ncompose:\n%s", compose)
	}
}

func TestGenerate_RedisService_StoreRedis(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:  config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		RateLimit: config.RateLimitConfig{Store: "redis"},
	}
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("redis:")) {
		t.Error("expected 'redis:' service when rate_limit.store is redis")
	}
	if !bytes.Contains(compose, []byte("redis:7-alpine")) {
		t.Error("expected redis:7-alpine image")
	}
	if !bytes.Contains(compose, []byte("redis-data:")) {
		t.Error("expected redis-data volume")
	}
	if !bytes.Contains(compose, []byte("VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS=redis:6379")) {
		t.Error("expected VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS env var in vibewarden service")
	}
}

func TestGenerate_RedisService_StoreMemory(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:  config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		RateLimit: config.RateLimitConfig{Store: "memory"},
	}
	compose := renderCompose(t, cfg)

	if bytes.Contains(compose, []byte("\n  redis:")) {
		t.Error("redis service must not be present when rate_limit.store is memory")
	}
	if bytes.Contains(compose, []byte("VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS")) {
		t.Error("VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS must not be present when store is memory")
	}
}

func TestGenerate_RedisService_DependsOn(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:  config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		RateLimit: config.RateLimitConfig{Store: "redis"},
	}
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("redis:\n        condition: service_healthy")) {
		t.Errorf("expected vibewarden depends_on redis with service_healthy\ncompose:\n%s", compose)
	}
}

func TestGenerate_RedisVolume_InVolumesSection(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:  config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		RateLimit: config.RateLimitConfig{Store: "redis"},
	}
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("volumes:")) {
		t.Error("expected volumes section when redis is enabled")
	}
	if !bytes.Contains(compose, []byte("  redis-data:")) {
		t.Error("expected redis-data volume in volumes section")
	}
}

func TestGenerate_SeedSecretsFile_GeneratedWhenNeeded(t *testing.T) {
	headers := []config.SecretsHeaderInjection{
		{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
	}
	cfg := secretsConfig(true, headers, nil)

	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	seedPath := filepath.Join(outputDir, "seed-secrets.sh")
	info, err := os.Stat(seedPath)
	if err != nil {
		t.Fatalf("expected seed-secrets.sh to exist: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o750 {
		t.Errorf("seed-secrets.sh permissions = %o, want 0750", perm)
	}
	data, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("reading seed-secrets.sh: %v", err)
	}
	if !bytes.Contains(data, []byte("bao kv put")) {
		t.Errorf("seed-secrets.sh does not contain 'bao kv put': %s", data)
	}
	if !bytes.Contains(data, []byte("app/api-key")) {
		t.Errorf("seed-secrets.sh does not contain secret path 'app/api-key': %s", data)
	}
}

func TestGenerate_SeedSecretsFile_NotGeneratedWhenNotNeeded(t *testing.T) {
	cfg := secretsConfig(true, nil, nil)

	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	seedPath := filepath.Join(outputDir, "seed-secrets.sh")
	if _, err := os.Stat(seedPath); err == nil {
		t.Error("seed-secrets.sh must not be generated when no inject entries are configured")
	}
}

func TestGenerate_SeedSecretsFile_NotGeneratedWhenSecretsDisabled(t *testing.T) {
	headers := []config.SecretsHeaderInjection{
		{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
	}
	cfg := secretsConfig(false, headers, nil)

	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	seedPath := filepath.Join(outputDir, "seed-secrets.sh")
	if _, err := os.Stat(seedPath); err == nil {
		t.Error("seed-secrets.sh must not be generated when secrets.enabled is false")
	}
}

func TestGenerate_SeedSecretsFile_ContainsBothHeadersAndEnv(t *testing.T) {
	headers := []config.SecretsHeaderInjection{
		{SecretPath: "app/api-key", SecretKey: "api_key", Header: "X-API-Key"},
	}
	env := []config.SecretsEnvInjection{
		{SecretPath: "app/db-pass", SecretKey: "password", EnvVar: "DB_PASSWORD"},
	}
	cfg := secretsConfig(true, headers, env)

	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	seedPath := filepath.Join(outputDir, "seed-secrets.sh")
	data, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("reading seed-secrets.sh: %v", err)
	}
	if !bytes.Contains(data, []byte("app/api-key")) {
		t.Errorf("seed-secrets.sh missing header secret path: %s", data)
	}
	if !bytes.Contains(data, []byte("X-API-Key")) {
		t.Errorf("seed-secrets.sh missing header name: %s", data)
	}
	if !bytes.Contains(data, []byte("app/db-pass")) {
		t.Errorf("seed-secrets.sh missing env secret path: %s", data)
	}
	if !bytes.Contains(data, []byte("DB_PASSWORD")) {
		t.Errorf("seed-secrets.sh missing env var name: %s", data)
	}
}

// observabilityConfig returns a Config with observability enabled and the
// given custom values. Zero values fall back to the supplied defaults.
func observabilityConfig(grafanaPort, prometheusPort, lokiPort, retentionDays int) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Observability: config.ObservabilityConfig{
			Enabled:        true,
			GrafanaPort:    grafanaPort,
			PrometheusPort: prometheusPort,
			LokiPort:       lokiPort,
			RetentionDays:  retentionDays,
		},
	}
}

func TestGenerate_Observability_WhenEnabled(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	expectedFiles := []string{
		filepath.Join(outputDir, "observability", "prometheus", "prometheus.yml"),
		filepath.Join(outputDir, "observability", "grafana", "provisioning", "datasources", "datasources.yml"),
		filepath.Join(outputDir, "observability", "grafana", "provisioning", "dashboards", "dashboards.yml"),
		filepath.Join(outputDir, "observability", "grafana", "dashboards", "vibewarden.json"),
		filepath.Join(outputDir, "observability", "loki", "loki-config.yml"),
		filepath.Join(outputDir, "observability", "promtail", "promtail-config.yml"),
		filepath.Join(outputDir, "observability", "otel-collector", "config.yaml"),
	}

	for _, f := range expectedFiles {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, err := os.Stat(f); err != nil {
				t.Errorf("expected file %q to exist: %v", f, err)
			}
		})
	}
}

func TestGenerate_Observability_WhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:      config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Observability: config.ObservabilityConfig{Enabled: false},
	}
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	obsDir := filepath.Join(outputDir, "observability")
	if _, err := os.Stat(obsDir); err == nil {
		t.Errorf("observability directory must not be created when observability is disabled: %q", obsDir)
	}
}

func TestGenerate_Observability_PrometheusConfig(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "observability", "prometheus", "prometheus.yml"))
	if err != nil {
		t.Fatalf("reading prometheus.yml: %v", err)
	}

	// Prometheus now scrapes the OTel Collector's Prometheus exporter, not VibeWarden directly.
	if !bytes.Contains(data, []byte("otel-collector:8889")) {
		t.Errorf("prometheus.yml should target otel-collector:8889, content:\n%s", data)
	}
	if !bytes.Contains(data, []byte("job_name: 'otel-collector'")) {
		t.Errorf("prometheus.yml should have job_name 'otel-collector', content:\n%s", data)
	}
	if bytes.Contains(data, []byte("/_vibewarden/metrics")) {
		t.Errorf("prometheus.yml must not scrape /_vibewarden/metrics directly when observability is enabled, content:\n%s", data)
	}
}

func TestGenerate_Observability_LokiRetention(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 14)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "observability", "loki", "loki-config.yml"))
	if err != nil {
		t.Fatalf("reading loki-config.yml: %v", err)
	}

	// 14 days * 24 hours = 336h
	if !bytes.Contains(data, []byte("336h")) {
		t.Errorf("loki-config.yml should have retention_period: 336h for 14 days, content:\n%s", data)
	}
}

func TestGenerate_Observability_GrafanaDatasources(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "observability", "grafana", "provisioning", "datasources", "datasources.yml"))
	if err != nil {
		t.Fatalf("reading datasources.yml: %v", err)
	}

	if !bytes.Contains(data, []byte("http://prometheus:9090")) {
		t.Errorf("datasources.yml should reference http://prometheus:9090, content:\n%s", data)
	}
	if !bytes.Contains(data, []byte("http://loki:3100")) {
		t.Errorf("datasources.yml should reference http://loki:3100, content:\n%s", data)
	}
}

func TestGenerate_Observability_Dashboard(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "observability", "grafana", "dashboards", "vibewarden.json"))
	if err != nil {
		t.Fatalf("reading vibewarden.json: %v", err)
	}

	// Verify it is valid JSON.
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		t.Errorf("vibewarden.json should be a JSON object, got: %q...", data[:min(50, len(data))])
	}
	if len(data) == 0 {
		t.Error("vibewarden.json should not be empty")
	}
}

func TestGenerate_Observability_ComposeServices(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	for _, svc := range []string{"prometheus:", "loki:", "promtail:", "otel-collector:", "grafana:"} {
		if !bytes.Contains(compose, []byte(svc)) {
			t.Errorf("expected service %q in compose output when observability enabled", svc)
		}
	}
}

func TestGenerate_Observability_ComposeProfiles(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	// All five observability services should carry the observability profile.
	count := bytes.Count(compose, []byte("- observability"))
	if count < 5 {
		t.Errorf("expected at least 5 occurrences of '- observability' profile annotation, got %d\ncompose:\n%s", count, compose)
	}
}

func TestGenerate_Observability_ComposeVolumes(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	for _, vol := range []string{"prometheus-data:", "loki-data:", "grafana-data:"} {
		if !bytes.Contains(compose, []byte(vol)) {
			t.Errorf("expected volume %q in compose volumes section, content:\n%s", vol, compose)
		}
	}
}

func TestGenerate_Observability_ComposePorts(t *testing.T) {
	cfg := observabilityConfig(3001, 9091, 3101, 7)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte(`"9091:9090"`)) {
		t.Errorf("expected prometheus port mapping 9091:9090\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte(`"3101:3100"`)) {
		t.Errorf("expected loki port mapping 3101:3100\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte(`"3001:3000"`)) {
		t.Errorf("expected grafana port mapping 3001:3000\ncompose:\n%s", compose)
	}
}

func TestGenerate_Observability_ComposeDependsOn(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	// Promtail depends on loki being healthy.
	if !bytes.Contains(compose, []byte("loki:\n        condition: service_healthy")) {
		t.Errorf("expected promtail depends_on loki with service_healthy\ncompose:\n%s", compose)
	}
	// Grafana depends on prometheus and loki being healthy.
	if !bytes.Contains(compose, []byte("prometheus:\n        condition: service_healthy")) {
		t.Errorf("expected grafana depends_on prometheus with service_healthy\ncompose:\n%s", compose)
	}
}

func TestGenerate_Observability_NotPresent_WhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:      config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Observability: config.ObservabilityConfig{Enabled: false},
	}
	compose := renderCompose(t, cfg)

	for _, svc := range []string{"prometheus:", "grafana:", "loki:", "promtail:", "otel-collector:"} {
		if bytes.Contains(compose, []byte(svc)) {
			t.Errorf("service %q must not appear when observability is disabled\ncompose:\n%s", svc, compose)
		}
	}
	for _, vol := range []string{"prometheus-data:", "loki-data:", "grafana-data:"} {
		if bytes.Contains(compose, []byte(vol)) {
			t.Errorf("volume %q must not appear when observability is disabled\ncompose:\n%s", vol, compose)
		}
	}
}

// --- OTel Collector tests ---

func TestGenerate_OtelCollector_ConfigFileCreated(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	configPath := filepath.Join(outputDir, "observability", "otel-collector", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected otel-collector config.yaml to exist at %q: %v", configPath, err)
	}
}

func TestGenerate_OtelCollector_ConfigContainsReceivers(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "observability", "otel-collector", "config.yaml"))
	if err != nil {
		t.Fatalf("reading otel-collector config.yaml: %v", err)
	}

	checks := []struct {
		desc    string
		contain []byte
	}{
		{"OTLP receiver", []byte("otlp:")},
		{"OTLP HTTP endpoint", []byte("0.0.0.0:4318")},
		{"Prometheus exporter endpoint", []byte("0.0.0.0:8889")},
		{"Loki exporter endpoint", []byte("http://loki:3100/loki/api/v1/push")},
		{"metrics pipeline", []byte("metrics:")},
		{"logs pipeline", []byte("logs:")},
		{"batch processor", []byte("batch:")},
	}

	for _, c := range checks {
		if !bytes.Contains(data, c.contain) {
			t.Errorf("otel-collector config.yaml missing %s (%q), content:\n%s", c.desc, c.contain, data)
		}
	}
}

func TestGenerate_OtelCollector_ComposeService(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("otel/opentelemetry-collector-contrib:")) {
		t.Errorf("expected otel-collector-contrib image in compose output\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte("otel-collector/config.yaml:/etc/otelcol-contrib/config.yaml")) {
		t.Errorf("expected otel-collector config volume mount\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte("localhost:13133")) {
		t.Errorf("expected otel-collector healthcheck on port 13133\ncompose:\n%s", compose)
	}
}

func TestGenerate_OtelCollector_ComposeDependsOn(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	// otel-collector depends on loki being healthy.
	// grafana depends on otel-collector being healthy.
	if !bytes.Contains(compose, []byte("otel-collector:\n        condition: service_healthy")) {
		t.Errorf("expected grafana depends_on otel-collector with service_healthy\ncompose:\n%s", compose)
	}
}

func TestGenerate_OtelCollector_OtlpEnvVars(t *testing.T) {
	cfg := observabilityConfig(3001, 9090, 3100, 7)
	compose := renderCompose(t, cfg)

	if !bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true")) {
		t.Errorf("expected VIBEWARDEN_TELEMETRY_OTLP_ENABLED=true in compose when observability enabled\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT=http://otel-collector:4318")) {
		t.Errorf("expected VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT env var pointing to collector\ncompose:\n%s", compose)
	}
	if !bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_LOGS_OTLP=true")) {
		t.Errorf("expected VIBEWARDEN_TELEMETRY_LOGS_OTLP=true in compose when observability enabled\ncompose:\n%s", compose)
	}
}

func TestGenerate_OtelCollector_OtlpEnvVars_AbsentWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:      config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Observability: config.ObservabilityConfig{Enabled: false},
	}
	compose := renderCompose(t, cfg)

	if bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_OTLP_ENABLED")) {
		t.Errorf("VIBEWARDEN_TELEMETRY_OTLP_ENABLED must not appear when observability is disabled\ncompose:\n%s", compose)
	}
	if bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT")) {
		t.Errorf("VIBEWARDEN_TELEMETRY_OTLP_ENDPOINT must not appear when observability is disabled\ncompose:\n%s", compose)
	}
	if bytes.Contains(compose, []byte("VIBEWARDEN_TELEMETRY_LOGS_OTLP")) {
		t.Errorf("VIBEWARDEN_TELEMETRY_LOGS_OTLP must not appear when observability is disabled\ncompose:\n%s", compose)
	}
}

func TestGenerate_OtelCollector_NotPresent_WhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:      config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Observability: config.ObservabilityConfig{Enabled: false},
	}
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	collectorDir := filepath.Join(outputDir, "observability", "otel-collector")
	if _, err := os.Stat(collectorDir); err == nil {
		t.Errorf("otel-collector directory must not be created when observability is disabled: %q", collectorDir)
	}
}

// --- Credential generation tests ---

// fakeCredentialGenerator is a test double for ports.CredentialGenerator.
type fakeCredentialGenerator struct {
	creds *gendomain.GeneratedCredentials
	err   error
}

func (f *fakeCredentialGenerator) Generate(_ context.Context) (*gendomain.GeneratedCredentials, error) {
	return f.creds, f.err
}

// fakeCredentialStore is a test double for ports.CredentialStore.
type fakeCredentialStore struct {
	written  *gendomain.GeneratedCredentials
	writeErr error
}

func (f *fakeCredentialStore) Write(_ context.Context, creds *gendomain.GeneratedCredentials, _ string) error {
	f.written = creds
	return f.writeErr
}

func (f *fakeCredentialStore) Read(_ context.Context, _ string) (*gendomain.GeneratedCredentials, error) {
	return f.written, nil
}

// fixedCreds returns a deterministic GeneratedCredentials for testing.
func fixedCreds() *gendomain.GeneratedCredentials {
	return &gendomain.GeneratedCredentials{
		PostgresPassword:     "fixed_postgres_pass_32chars_abcd",
		KratosCookieSecret:   "fixed_cookie_secret_32chars_abcd",
		KratosCipherSecret:   "fixed_cipher_secret_32chars_abcd",
		GrafanaAdminPassword: "fixed_grafana_pass24ab",
		OpenBaoDevRootToken:  "fixed_openbao_token_32chars_abcd",
	}
}

func TestGenerate_ProdProfile_RequiresSecrets(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		&fakeCredentialStore{},
	)
	cfg := minimalConfig()
	cfg.Profile = "prod"
	cfg.Secrets.Enabled = false

	err := svc.Generate(context.Background(), cfg, outputDir)
	if err == nil {
		t.Fatal("Generate() expected error for prod profile without secrets.enabled, got nil")
	}
	if !strings.Contains(err.Error(), "prod profile requires secrets.enabled") {
		t.Errorf("Generate() error = %q, want message about prod profile requiring secrets", err.Error())
	}
}

func TestGenerate_DevProfile_AllowsNoSecrets(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		&fakeCredentialStore{},
	)
	cfg := minimalConfig()
	cfg.Profile = "dev"
	cfg.Secrets.Enabled = false

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Errorf("Generate() unexpected error for dev profile without secrets: %v", err)
	}
}

func TestGenerate_TLSProfile_AllowsNoSecrets(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		&fakeCredentialStore{},
	)
	cfg := minimalConfig()
	cfg.Profile = "tls"
	cfg.Secrets.Enabled = false

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Errorf("Generate() unexpected error for tls profile without secrets: %v", err)
	}
}

func TestGenerate_ProdProfile_WithSecretsEnabled_Succeeds(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		&fakeCredentialStore{},
	)
	cfg := minimalConfig()
	cfg.Profile = "prod"
	cfg.Secrets.Enabled = true

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Errorf("Generate() unexpected error for prod profile with secrets.enabled: %v", err)
	}
}

func TestGenerate_CredentialsWritten(t *testing.T) {
	outputDir := t.TempDir()
	store := &fakeCredentialStore{}
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		store,
	)

	if err := svc.Generate(context.Background(), minimalConfig(), outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	if store.written == nil {
		t.Fatal("CredentialStore.Write() was not called")
	}
	if store.written.PostgresPassword != fixedCreds().PostgresPassword {
		t.Errorf("stored PostgresPassword = %q, want %q", store.written.PostgresPassword, fixedCreds().PostgresPassword)
	}
}

func TestGenerate_CredentialGeneratorError_ReturnsError(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{err: fmt.Errorf("rand failure")},
		&fakeCredentialStore{},
	)

	err := svc.Generate(context.Background(), minimalConfig(), outputDir)
	if err == nil {
		t.Fatal("Generate() expected error when credential generator fails, got nil")
	}
	if !strings.Contains(err.Error(), "generating credentials") {
		t.Errorf("Generate() error = %q, expected context about credential generation", err.Error())
	}
}

func TestGenerate_CredentialStoreError_ReturnsError(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewServiceWithCredentials(
		&fakeRenderer{},
		&fakeCredentialGenerator{creds: fixedCreds()},
		&fakeCredentialStore{writeErr: fmt.Errorf("disk full")},
	)

	err := svc.Generate(context.Background(), minimalConfig(), outputDir)
	if err == nil {
		t.Fatal("Generate() expected error when credential store fails, got nil")
	}
	if !strings.Contains(err.Error(), "writing credentials") {
		t.Errorf("Generate() error = %q, expected context about credential write", err.Error())
	}
}

func TestGenerate_EnvTemplateWritten(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	cfg := minimalConfig()

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	envTemplatePath := filepath.Join(outputDir, ".env.template")
	if _, err := os.Stat(envTemplatePath); err != nil {
		t.Errorf("expected .env.template to exist: %v", err)
	}
}

func TestGenerate_EnvTemplateNoSecrets(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	cfg := minimalConfig()
	cfg.Profile = "dev"

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".env.template"))
	if err != nil {
		t.Fatalf("reading .env.template: %v", err)
	}

	// Verify .env.template contains no credential-like keys.
	forbiddenKeys := []string{
		"POSTGRES_PASSWORD=",
		"KRATOS_SECRETS_COOKIE=",
		"KRATOS_SECRETS_CIPHER=",
		"GRAFANA_ADMIN_PASSWORD=",
		"OPENBAO_DEV_ROOT_TOKEN=",
	}
	for _, key := range forbiddenKeys {
		if bytes.Contains(data, []byte(key)) {
			t.Errorf(".env.template contains forbidden credential key %q", key)
		}
	}
}

func TestGenerate_EnvTemplateContainsProfile(t *testing.T) {
	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	cfg := minimalConfig()
	cfg.Profile = "tls"

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".env.template"))
	if err != nil {
		t.Fatalf("reading .env.template: %v", err)
	}

	if !bytes.Contains(data, []byte("VIBEWARDEN_PROFILE=tls")) {
		t.Errorf(".env.template should contain VIBEWARDEN_PROFILE=tls, content:\n%s", data)
	}
}

func TestGenerate_SeedSecretsSourcesCredentials(t *testing.T) {
	headers := []config.SecretsHeaderInjection{
		{SecretPath: "app/api-key", SecretKey: "value", Header: "X-API-Key"},
	}
	cfg := secretsConfig(true, headers, nil)

	outputDir := t.TempDir()
	svc := generate.NewService(realRenderer())
	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "seed-secrets.sh"))
	if err != nil {
		t.Fatalf("reading seed-secrets.sh: %v", err)
	}

	// Verify the script sources the .credentials file.
	if !bytes.Contains(data, []byte(".credentials")) {
		t.Errorf("seed-secrets.sh should reference .credentials file, content:\n%s", data)
	}
	if !bytes.Contains(data, []byte(". \"$CREDS_FILE\"")) {
		t.Errorf("seed-secrets.sh should source credentials via '. \"$CREDS_FILE\"', content:\n%s", data)
	}
}

func TestGenerate_NoCredentialAdapters_SkipsCredentials(t *testing.T) {
	// When NewService (no credential adapters) is used, the .credentials file
	// must not be written; Generate should still succeed.
	outputDir := t.TempDir()
	svc := generate.NewService(&fakeRenderer{})
	cfg := minimalConfig()

	if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	credPath := filepath.Join(outputDir, ".credentials")
	if _, err := os.Stat(credPath); err == nil {
		t.Error("expected .credentials NOT to exist when no credential adapters are configured")
	}
}
