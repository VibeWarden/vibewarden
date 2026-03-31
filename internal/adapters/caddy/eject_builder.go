package caddy

import (
	"github.com/vibewarden/vibewarden/internal/ports"
)

// EjectBuilder implements the eject.ConfigBuilder interface using the Caddy
// JSON configuration builder. It wraps BuildCaddyConfig so that the eject
// application service can depend on the interface rather than the concrete
// caddy package.
type EjectBuilder struct{}

// NewEjectBuilder returns a new EjectBuilder.
func NewEjectBuilder() *EjectBuilder {
	return &EjectBuilder{}
}

// Build delegates to BuildCaddyConfig and satisfies eject.ConfigBuilder.
func (b *EjectBuilder) Build(cfg *ports.ProxyConfig) (map[string]any, error) {
	return BuildCaddyConfig(cfg)
}
