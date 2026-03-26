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

	// Generate docker-compose.yml.
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := s.renderer.RenderToFile("docker-compose.yml.tmpl", cfg, composePath, true); err != nil {
		return fmt.Errorf("rendering docker-compose.yml: %w", err)
	}

	return nil
}

// resolveIdentitySchema returns the JSON bytes for the identity schema.
// Precedence: overrides.identity_schema path > auth.identity_schema preset/path.
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

	data, err := presets.Resolve(schemaName)
	if err != nil {
		return nil, fmt.Errorf("resolving preset %q: %w", schemaName, err)
	}
	return data, nil
}
