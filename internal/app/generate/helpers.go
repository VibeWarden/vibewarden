// Package generate provides the application service that generates
// VibeWarden runtime configuration files from a vibewarden.yaml Config.
package generate

import "github.com/vibewarden/vibewarden/internal/config"

// NeedsOpenBao returns true if the config requires an OpenBao service in the
// generated docker-compose.yml. This is the case when the secrets plugin is enabled.
func NeedsOpenBao(cfg *config.Config) bool {
	return cfg.Secrets.Enabled
}

// NeedsRedis returns true if the config requires a Redis service in the
// generated docker-compose.yml. This is the case when rate limiting uses the
// Redis backing store.
func NeedsRedis(cfg *config.Config) bool {
	return cfg.RateLimit.Store == "redis"
}

// NeedsSeedSecrets returns true if dev mode should seed OpenBao with demo
// secrets. This is true when the secrets plugin is enabled AND at least one
// header or env injection entry is configured.
func NeedsSeedSecrets(cfg *config.Config) bool {
	if !cfg.Secrets.Enabled {
		return false
	}
	return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}
