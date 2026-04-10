package plugin

// PluginConfig holds the configuration block for a single plugin as loaded
// from vibewarden.yaml under plugins.<name>. It is intentionally generic so
// that each plugin can define its own settings structure and unmarshal into it.
//
//nolint:revive // PluginConfig is the established public API name used across all plugin implementations
type PluginConfig struct {
	// Enabled controls whether the plugin is active. Disabled plugins are
	// never initialised or started.
	Enabled bool `mapstructure:"enabled"`

	// Settings holds the plugin-specific configuration keys as a free-form
	// map. Each plugin is responsible for interpreting these values.
	Settings map[string]any `mapstructure:",remain"`
}
