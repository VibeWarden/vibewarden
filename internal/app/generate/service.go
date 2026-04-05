// Package generate provides the application service that generates
// VibeWarden runtime configuration files from a vibewarden.yaml Config.
package generate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/presets"
	"github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const defaultOutputDir = ".vibewarden/generated"

// File permission constants used throughout this package.
const (
	// permDir is the permission mode for generated directories.
	// Group and world execute bits are omitted to prevent unintended access.
	permDir = os.FileMode(0o750)
	// permConfig is the permission mode for generated config/data files.
	// Readable only by the owner to protect credentials embedded in kratos.yml.
	permConfig = os.FileMode(0o600)
)

// Service implements ports.ConfigGenerator using a ports.TemplateRenderer.
type Service struct {
	renderer  ports.TemplateRenderer
	credGen   ports.CredentialGenerator
	credStore ports.CredentialStore
}

// NewService creates a generate Service that uses renderer to execute the
// embedded Kratos and Docker Compose templates. credGen and credStore may be
// nil; when nil, credential generation and persistence are skipped.
func NewService(renderer ports.TemplateRenderer) *Service {
	return &Service{renderer: renderer}
}

// NewServiceWithCredentials creates a generate Service with credential
// generation and storage support.
func NewServiceWithCredentials(
	renderer ports.TemplateRenderer,
	credGen ports.CredentialGenerator,
	credStore ports.CredentialStore,
) *Service {
	return &Service{
		renderer:  renderer,
		credGen:   credGen,
		credStore: credStore,
	}
}

// Generate implements ports.ConfigGenerator.
// It writes the following files under outputDir (default: ".vibewarden/generated"):
//
//	.credentials         (mode 0600, when credGen and credStore are configured)
//	.env.template        (non-secret config, safe to commit)
//	kratos/kratos.yml
//	kratos/identity.schema.json
//	kratos/mappers/<provider>.jsonnet  (one per configured social provider)
//	docker-compose.yml
func (s *Service) Generate(ctx context.Context, cfg *config.Config, outputDir string) error {
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	// Sanitise the caller-supplied output directory to prevent path traversal.
	outputDir = filepath.Clean(outputDir)

	// Warn when prod profile is used without secrets enabled.
	// OpenBao is strongly recommended for production but is no longer mandatory
	// so that operators who manage secrets externally are not blocked.
	if cfg.Profile == "prod" && !cfg.Secrets.Enabled {
		slog.Warn("prod profile is running without secrets.enabled: true; OpenBao is strongly recommended for production secret management")
	}

	// Emit egress-related warnings to stderr via slog.
	for _, w := range cfg.Egress.EgressWarnings() {
		slog.Warn(w)
	}

	// Warn when network isolation is enabled but the app is not containerized.
	// A host-mode app bypasses Docker network isolation entirely.
	if cfg.Egress.IsNetworkIsolationEnabled() && cfg.App.Build == "" && cfg.App.Image == "" {
		slog.Warn("Network isolation is enabled but app.build and app.image are both empty: host-mode app bypasses Docker network isolation")
	}

	// kratosMode is true only when auth is enabled and mode is "kratos" and
	// the Kratos instance is not managed externally.
	// An empty mode string is also treated as Kratos for defensive backwards
	// compatibility with code that constructs config.Config structs directly.
	// When kratosMode is false, Kratos-specific files are not generated.
	kratosMode := cfg.Auth.Enabled && cfg.Auth.Mode == config.AuthModeKratos && !cfg.Kratos.External

	if kratosMode {
		if err := os.MkdirAll(filepath.Join(outputDir, "kratos"), permDir); err != nil {
			return fmt.Errorf("creating output directories: %w", err)
		}
	}

	// Generate and persist credentials when the adapters are configured.
	if s.credGen != nil && s.credStore != nil {
		creds, err := s.credGen.Generate(ctx)
		if err != nil {
			return fmt.Errorf("generating credentials: %w", err)
		}
		if err := s.credStore.Write(ctx, creds, outputDir); err != nil {
			return fmt.Errorf("writing credentials: %w", err)
		}
		// Docker Compose uses .env next to docker-compose.yml for variable
		// interpolation. Write the same credentials as .env so ${POSTGRES_PASSWORD}
		// etc. are resolved in the compose YAML.
		envContent := fmt.Sprintf(
			"POSTGRES_PASSWORD=%s\nKRATOS_SECRETS_COOKIE=%s\nKRATOS_SECRETS_CIPHER=%s\nGRAFANA_ADMIN_PASSWORD=%s\nOPENBAO_DEV_ROOT_TOKEN=%s\n",
			creds.PostgresPassword, creds.KratosCookieSecret, creds.KratosCipherSecret,
			creds.GrafanaAdminPassword, creds.OpenBaoDevRootToken,
		)
		if err := os.WriteFile(filepath.Join(outputDir, ".env"), []byte(envContent), permConfig); err != nil {
			return fmt.Errorf("writing .env: %w", err)
		}
	}

	// Render .env.template (non-secret config, safe to commit).
	envTemplatePath := filepath.Join(outputDir, ".env.template")
	if err := s.renderer.RenderToFile("env.template.tmpl", cfg, envTemplatePath, true); err != nil {
		return fmt.Errorf("rendering .env.template: %w", err)
	}

	// Kratos configuration files are only generated when mode is "kratos".
	// When auth.mode is "jwt", "api-key", or "none", no Kratos files are written.
	if kratosMode {
		// Resolve identity schema — override path takes priority over the preset
		// selected by auth.identity_schema.
		schemaJSON, err := resolveIdentitySchema(cfg)
		if err != nil {
			return fmt.Errorf("resolving identity schema: %w", err)
		}

		// Write identity.schema.json.
		schemaPath := filepath.Join(outputDir, "kratos", "identity.schema.json")
		if err := os.WriteFile(schemaPath, schemaJSON, permConfig); err != nil {
			return fmt.Errorf("writing identity schema: %w", err)
		}

		// Generate kratos.yml unless an override path is configured.
		kratosYMLPath := filepath.Join(outputDir, "kratos", "kratos.yml")
		if cfg.Overrides.KratosConfig != "" {
			// Sanitise the user-supplied override path before reading from it.
			overridePath := filepath.Clean(cfg.Overrides.KratosConfig)
			data, err := os.ReadFile(overridePath)
			if err != nil {
				return fmt.Errorf("reading kratos override config %q: %w", overridePath, err)
			}
			// kratosYMLPath is constructed from the already-cleaned outputDir and
			// constant path segments — the destination is safe. The G703 taint
			// tracks the content bytes read from the override file, which is a
			// false positive for path traversal in the write destination.
			if err := os.WriteFile(kratosYMLPath, data, permConfig); err != nil { //#nosec G703 -- destination is filepath.Join(cleanOutputDir, "kratos", "kratos.yml")
				return fmt.Errorf("writing kratos.yml from override: %w", err)
			}
		} else {
			if err := s.renderer.RenderToFile("kratos.yml.tmpl", cfg, kratosYMLPath, true); err != nil {
				return fmt.Errorf("rendering kratos.yml: %w", err)
			}
		}

		// Generate OIDC mapper files if social providers are configured.
		if len(cfg.Auth.SocialProviders) > 0 {
			if err := s.generateMappers(cfg, outputDir); err != nil {
				return fmt.Errorf("generating OIDC mappers: %w", err)
			}
		}
	}

	// Generate docker-compose.yml unless an override path is configured.
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if cfg.Overrides.ComposeFile != "" {
		// Sanitise the user-supplied override path before reading from it.
		overridePath := filepath.Clean(cfg.Overrides.ComposeFile)
		data, err := os.ReadFile(overridePath)
		if err != nil {
			return fmt.Errorf("reading compose override config %q: %w", overridePath, err)
		}
		// composePath is constructed from the already-cleaned outputDir and a
		// constant — the destination is safe. G703 taint tracks content bytes.
		if err := os.WriteFile(composePath, data, permConfig); err != nil { //#nosec G703 -- destination is filepath.Join(cleanOutputDir, "docker-compose.yml")
			return fmt.Errorf("writing docker-compose.yml from override: %w", err)
		}
	} else {
		if err := s.renderer.RenderToFile("docker-compose.yml.tmpl", cfg, composePath, true); err != nil {
			return fmt.Errorf("rendering docker-compose.yml: %w", err)
		}
	}

	// Generate openbao/config.hcl when the secrets plugin is enabled and the
	// profile is prod. The file configures OpenBao in server mode with file
	// storage backend and is mounted read-only into the openbao container.
	if NeedsOpenBaoConfig(cfg) {
		openbaoDir := filepath.Join(outputDir, "openbao")
		if err := os.MkdirAll(openbaoDir, permDir); err != nil {
			return fmt.Errorf("creating openbao directory: %w", err)
		}
		hclPath := filepath.Join(openbaoDir, "config.hcl")
		if err := s.renderer.RenderToFile("openbao-config.hcl.tmpl", cfg, hclPath, true); err != nil {
			return fmt.Errorf("rendering openbao/config.hcl: %w", err)
		}
	}

	// Generate seed-secrets.sh when the secrets plugin is enabled and has
	// inject entries. The script is mounted into the seed-secrets init container
	// defined in docker-compose.yml to populate OpenBao with demo values.
	if NeedsSeedSecrets(cfg) {
		seedPath := filepath.Join(outputDir, "seed-secrets.sh")
		if err := s.renderer.RenderToFile("seed-secrets.sh.tmpl", cfg, seedPath, true); err != nil {
			return fmt.Errorf("rendering seed-secrets.sh: %w", err)
		}
		if err := os.Chmod(seedPath, 0o750); err != nil { //nolint:gosec // seed-secrets.sh must be executable; 0o750 is intentional for a shell script
			return fmt.Errorf("setting seed-secrets.sh permissions: %w", err)
		}
	}

	// Generate observability configs when enabled.
	if cfg.Observability.Enabled {
		if err := s.generateObservability(cfg, outputDir); err != nil {
			return fmt.Errorf("generating observability configs: %w", err)
		}
	}

	return nil
}

// generateObservability writes all observability config files to
// <outputDir>/observability/.
func (s *Service) generateObservability(cfg *config.Config, outputDir string) error {
	obsDir := filepath.Join(outputDir, "observability")

	// Create directory structure.
	dirs := []string{
		filepath.Join(obsDir, "prometheus"),
		filepath.Join(obsDir, "grafana", "provisioning", "datasources"),
		filepath.Join(obsDir, "grafana", "provisioning", "dashboards"),
		filepath.Join(obsDir, "grafana", "dashboards"),
		filepath.Join(obsDir, "loki"),
		filepath.Join(obsDir, "promtail"),
		filepath.Join(obsDir, "otel-collector"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, permDir); err != nil {
			return fmt.Errorf("creating directory %q: %w", dir, err)
		}
	}

	// Render Prometheus config.
	if err := s.renderer.RenderToFile(
		"observability/prometheus.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "prometheus", "prometheus.yml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering prometheus.yml: %w", err)
	}

	// Render Grafana datasources.
	if err := s.renderer.RenderToFile(
		"observability/grafana-datasources.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "grafana", "provisioning", "datasources", "datasources.yml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering grafana datasources: %w", err)
	}

	// Render Grafana dashboard provisioner.
	if err := s.renderer.RenderToFile(
		"observability/grafana-dashboards.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "grafana", "provisioning", "dashboards", "dashboards.yml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering grafana dashboard provisioner: %w", err)
	}

	// Copy Grafana dashboard JSON (static, not a template).
	dashboardJSON, err := templates.FS.ReadFile("observability/vibewarden-dashboard.json")
	if err != nil {
		return fmt.Errorf("reading embedded dashboard JSON: %w", err)
	}
	dashboardPath := filepath.Join(obsDir, "grafana", "dashboards", "vibewarden.json")
	if err := os.WriteFile(dashboardPath, dashboardJSON, permConfig); err != nil {
		return fmt.Errorf("writing dashboard JSON: %w", err)
	}

	// Render Loki config.
	if err := s.renderer.RenderToFile(
		"observability/loki-config.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "loki", "loki-config.yml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering loki-config.yml: %w", err)
	}

	// Render Promtail config.
	if err := s.renderer.RenderToFile(
		"observability/promtail-config.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "promtail", "promtail-config.yml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering promtail-config.yml: %w", err)
	}

	// Render OTel Collector config.
	if err := s.renderer.RenderToFile(
		"observability/otel-collector-config.yml.tmpl",
		cfg,
		filepath.Join(obsDir, "otel-collector", "config.yaml"),
		true,
	); err != nil {
		return fmt.Errorf("rendering otel-collector config: %w", err)
	}

	return nil
}

// generateMappers writes Kratos OIDC Jsonnet mapper files to
// <outputDir>/kratos/mappers/ — one file per unique mapper required by the
// configured social providers.
//
// Mapper selection:
//   - google  → mappers/google.jsonnet
//   - github  → mappers/github.jsonnet
//   - all others → mappers/generic.jsonnet
//
// Files are written only for mappers that are actually referenced so the
// output directory stays minimal.
func (s *Service) generateMappers(cfg *config.Config, outputDir string) error {
	mappersDir := filepath.Join(outputDir, "kratos", "mappers")
	if err := os.MkdirAll(mappersDir, permDir); err != nil {
		return fmt.Errorf("creating mappers directory: %w", err)
	}

	// Collect the set of mapper file names required by the configuration.
	needed := make(map[string]bool)
	for _, sp := range cfg.Auth.SocialProviders {
		needed[mapperFileName(sp.Provider)] = true
	}

	// Write each required mapper file from the embedded FS.
	// Guard against path traversal: reject any mapper file name that would
	// escape the mappers directory after cleaning.
	for mapperFile := range needed {
		if strings.ContainsAny(mapperFile, `/\`) || mapperFile == ".." {
			return fmt.Errorf("invalid mapper file name %q: path separators not allowed", mapperFile)
		}
		src := "mappers/" + mapperFile
		data, err := templates.FS.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading embedded mapper %q: %w", src, err)
		}
		dst := filepath.Join(mappersDir, mapperFile)
		if err := os.WriteFile(dst, data, permConfig); err != nil {
			return fmt.Errorf("writing mapper file %q: %w", dst, err)
		}
	}
	return nil
}

// mapperFileName returns the Jsonnet mapper file name for the given provider name.
// Google and GitHub have dedicated mappers; all other providers use the generic mapper.
func mapperFileName(provider string) string {
	switch provider {
	case "google":
		return "google.jsonnet"
	case "github":
		return "github.jsonnet"
	default:
		return "generic.jsonnet"
	}
}

// resolveIdentitySchema returns the JSON bytes for the identity schema.
//
// Precedence (highest to lowest):
//  1. overrides.identity_schema — explicit filesystem path, used verbatim.
//  2. auth.identity_schema — explicit preset name or filesystem path chosen
//     by the operator.
//  3. Auto-selection: when social providers are configured and
//     auth.identity_schema is "email_password" (the default), the service
//     upgrades to the "social" preset so that name and picture traits are
//     available for OIDC mappers to populate.
//  4. Fallback: "email_password" preset when auth.identity_schema is empty.
func resolveIdentitySchema(cfg *config.Config) ([]byte, error) {
	if cfg.Overrides.IdentitySchema != "" {
		data, err := os.ReadFile(cfg.Overrides.IdentitySchema)
		if err != nil {
			return nil, fmt.Errorf("reading identity schema override %q: %w", cfg.Overrides.IdentitySchema, err)
		}
		return data, nil
	}

	schemaName := cfg.Auth.IdentitySchema
	if schemaName == "" {
		schemaName = presets.PresetEmailPassword
	}

	// Auto-upgrade to the social preset when social providers are configured
	// and the operator has not explicitly chosen a schema other than the
	// default email_password preset.
	if len(cfg.Auth.SocialProviders) > 0 && schemaName == presets.PresetEmailPassword {
		schemaName = presets.PresetSocial
	}

	data, err := presets.Resolve(schemaName)
	if err != nil {
		return nil, fmt.Errorf("resolving preset %q: %w", schemaName, err)
	}
	return data, nil
}
