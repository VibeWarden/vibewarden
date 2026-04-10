// Package generate provides the application service that generates
// VibeWarden runtime configuration files from a vibewarden.yaml Config.
package generate

import "github.com/vibewarden/vibewarden/internal/config"

// NeedsOpenBao returns true if the config requires an OpenBao service in the
// generated docker-compose.yml. This is the case when the secrets plugin is enabled.
func NeedsOpenBao(cfg *config.Config) bool {
	return cfg.Secrets.Enabled
}

// NeedsRedis returns true if the config requires a local Redis service in the
// generated docker-compose.yml. This is the case when rate limiting uses the
// Redis backing store AND no external Redis URL has been configured.
// When rate_limit.redis.url points to an external instance, the local Redis
// container is omitted from the generated Compose file.
func NeedsRedis(cfg *config.Config) bool {
	return cfg.RateLimit.Store == "redis" && !cfg.RateLimit.Redis.HasExternalURL()
}

// NeedsObservability returns true if the config requires the observability
// stack (Prometheus, Grafana, Loki, Promtail) in the generated compose.
func NeedsObservability(cfg *config.Config) bool {
	return cfg.Observability.Enabled
}

// NeedsOpenBaoConfig returns true if an openbao/config.hcl file should be
// generated. This is the case when the secrets plugin is enabled and the
// deployment profile is "prod" — OpenBao runs in server mode and requires an
// explicit HCL configuration file. In dev mode no config file is needed.
func NeedsOpenBaoConfig(cfg *config.Config) bool {
	return cfg.Secrets.Enabled && cfg.Profile == "prod"
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
