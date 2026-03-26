// Package generate provides the application service that generates
// VibeWarden runtime configuration files from a vibewarden.yaml Config.
package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/presets"
	"github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const defaultOutputDir = ".vibewarden/generated"

// Service implements ports.ConfigGenerator using a ports.TemplateRenderer.
type Service struct {
	renderer ports.TemplateRenderer
}

// NewService creates a generate Service that uses renderer to execute the
// embedded Kratos and Docker Compose templates.
func NewService(renderer ports.TemplateRenderer) *Service {
	return &Service{renderer: renderer}
}

// Generate implements ports.ConfigGenerator.
// It writes the following files under outputDir (default: ".vibewarden/generated"):
//
//	kratos/kratos.yml
//	kratos/identity.schema.json
//	kratos/mappers/<provider>.jsonnet  (one per configured social provider)
//	docker-compose.yml
func (s *Service) Generate(ctx context.Context, cfg *config.Config, outputDir string) error {
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	if err := os.MkdirAll(filepath.Join(outputDir, "kratos"), 0o755); err != nil {
		return fmt.Errorf("creating output directories: %w", err)
	}

	// Resolve identity schema — override path takes priority over the preset
	// selected by auth.identity_schema.
	schemaJSON, err := resolveIdentitySchema(cfg)
	if err != nil {
		return fmt.Errorf("resolving identity schema: %w", err)
	}

	// Write identity.schema.json.
	schemaPath := filepath.Join(outputDir, "kratos", "identity.schema.json")
	if err := os.WriteFile(schemaPath, schemaJSON, 0o644); err != nil {
		return fmt.Errorf("writing identity schema: %w", err)
	}

	// Generate kratos.yml unless an override path is configured.
	kratosYMLPath := filepath.Join(outputDir, "kratos", "kratos.yml")
	if cfg.Overrides.KratosConfig != "" {
		// Copy the override file verbatim.
		data, err := os.ReadFile(cfg.Overrides.KratosConfig)
		if err != nil {
			return fmt.Errorf("reading kratos override config %q: %w", cfg.Overrides.KratosConfig, err)
		}
		if err := os.WriteFile(kratosYMLPath, data, 0o644); err != nil {
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

	// Generate docker-compose.yml unless an override path is configured.
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if cfg.Overrides.ComposeFile != "" {
		// Copy the override file verbatim.
		data, err := os.ReadFile(cfg.Overrides.ComposeFile)
		if err != nil {
			return fmt.Errorf("reading compose override config %q: %w", cfg.Overrides.ComposeFile, err)
		}
		if err := os.WriteFile(composePath, data, 0o644); err != nil {
			return fmt.Errorf("writing docker-compose.yml from override: %w", err)
		}
	} else {
		if err := s.renderer.RenderToFile("docker-compose.yml.tmpl", cfg, composePath, true); err != nil {
			return fmt.Errorf("rendering docker-compose.yml: %w", err)
		}
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
	if err := os.MkdirAll(mappersDir, 0o755); err != nil {
		return fmt.Errorf("creating mappers directory: %w", err)
	}

	// Collect the set of mapper file names required by the configuration.
	needed := make(map[string]bool)
	for _, sp := range cfg.Auth.SocialProviders {
		needed[mapperFileName(sp.Provider)] = true
	}

	// Write each required mapper file from the embedded FS.
	for mapperFile := range needed {
		src := "mappers/" + mapperFile
		data, err := templates.FS.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading embedded mapper %q: %w", src, err)
		}
		dst := filepath.Join(mappersDir, mapperFile)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
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
