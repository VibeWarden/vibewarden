package ports

// ConfigBuilder is the outbound port for building a proxy configuration map
// from a ProxyConfig. The caddy adapter implements this interface for the
// eject use case.
type ConfigBuilder interface {
	// Build returns a proxy-format-specific configuration map.
	Build(cfg *ProxyConfig) (map[string]any, error)
}
