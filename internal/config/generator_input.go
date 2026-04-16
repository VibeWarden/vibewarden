// Package config holds configuration types and loading logic for VibeWarden.
package config

import "github.com/vibewarden/vibewarden/internal/ports"

// ToGeneratorInput maps a *Config into a ports.GeneratorInput.
//
// The decision fields are populated from the config's typed values so that
// ConfigGenerator implementations can branch on the port-owned DTO without
// importing internal/config. TemplateData carries the same *Config through to
// the template renderer — the generator casts it back at render time, so
// template output is byte-identical to passing the Config directly.
//
// The mapper lives in internal/config (not internal/ports) so that
// internal/ports continues to depend only on stdlib and internal/domain/*.
func (c *Config) ToGeneratorInput() ports.GeneratorInput {
	if c == nil {
		return ports.GeneratorInput{}
	}
	return ports.GeneratorInput{
		Profile:              c.Profile,
		AuthEnabled:          c.Auth.Enabled,
		AuthMode:             string(c.Auth.Mode),
		KratosExternal:       c.Kratos.External,
		SecretsEnabled:       c.Secrets.Enabled,
		ObservabilityEnabled: c.Observability.Enabled,
		TemplateData:         c,
	}
}
