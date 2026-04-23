package ports

// ConfigUpdater is an optional outbound port that a ProxyServer may implement
// to accept a new ProxyConfig before a Reload call. It allows the reload
// service to update the adapter's configuration without knowing the concrete
// type.
type ConfigUpdater interface {
	// UpdateConfig replaces the adapter's current ProxyConfig with cfg.
	UpdateConfig(cfg *ProxyConfig)
}
