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

// NeedsObservability returns true if the config requires the observability
// stack (Prometheus, Grafana, Loki, Promtail) in the generated compose.
func NeedsObservability(cfg *config.Config) bool {
	return cfg.Observability.Enabled
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

// NeedsLocalKratosDB returns true if the generated Docker Compose should
// include a local kratos-db Postgres container. This is false when an external
// database URL is configured (database.external_url), in which case the user
// provides and manages their own Postgres instance.
func NeedsLocalKratosDB(cfg *config.Config) bool {
	if !cfg.Auth.Enabled || cfg.Auth.Mode != "kratos" || cfg.Kratos.External {
		return false
	}
	return cfg.Database.ExternalURL == ""
}
