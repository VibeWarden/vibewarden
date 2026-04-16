package ports

import (
	"context"
)

// GeneratorInput is the port-owned DTO passed to ConfigGenerator.Generate.
//
// It decouples the ports package from internal/config: decision fields that
// drive service-side branching are expressed as primitive-typed values, while
// the full template payload is carried as an opaque TemplateData that the
// adapter (not the port) interprets.
//
// v1 note: the Generate service body continues to read directly from the
// concrete payload via a type assertion on TemplateData; the typed decision
// fields are declared here for the ports contract and are intentionally not
// consumed by the v1 service body. Future iterations will migrate the Generate
// body to branch on these fields and narrow TemplateData to a named template
// model.
type GeneratorInput struct {
	// Profile is the deployment profile, e.g. "dev" or "prod". It is used by
	// future service-side branching (e.g. to emit prod-only warnings or to
	// write openbao/config.hcl only in prod).
	Profile string

	// AuthEnabled reports whether authentication is turned on.
	AuthEnabled bool

	// AuthMode is the configured authentication mode, e.g. "kratos", "jwt",
	// "api-key", or "none".
	AuthMode string

	// KratosExternal reports whether Kratos is managed externally. When true
	// the generator must skip kratos.yml and identity-schema output.
	KratosExternal bool

	// SecretsEnabled reports whether the secrets plugin is active.
	SecretsEnabled bool

	// ObservabilityEnabled reports whether the observability stack should be
	// generated.
	ObservabilityEnabled bool

	// TemplateData is the opaque payload passed through to TemplateRenderer
	// calls. In v1 this is *config.Config; adapters that call Render /
	// RenderToFile pass TemplateData verbatim.
	TemplateData any
}

// ConfigGenerator generates VibeWarden runtime configuration files from a
// GeneratorInput. Implementations write files under the .vibewarden/generated/
// directory so that Docker Compose and Ory Kratos can pick them up.
type ConfigGenerator interface {
	// Generate creates or overwrites the generated configuration files for the
	// supplied input under outputDir. When outputDir is empty it defaults to
	// ".vibewarden/generated" relative to the current working directory.
	//
	// Generated files:
	//   <outputDir>/kratos/kratos.yml
	//   <outputDir>/kratos/identity.schema.json
	//   <outputDir>/docker-compose.yml
	Generate(ctx context.Context, input GeneratorInput, outputDir string) error
}
