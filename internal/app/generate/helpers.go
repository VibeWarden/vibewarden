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
// generated. This is the case when the secrets backend is OpenBao
// (secrets.store: openbao) AND the deployment profile is "prod" — OpenBao runs
// in server mode and requires an explicit HCL configuration file. In dev mode
// no config file is needed. When store is "builtin" (or unset) this always
// returns false; writing an openbao/config.hcl with no OpenBao service to
// consume it would be a dangling artifact.
func NeedsOpenBaoConfig(cfg *config.Config) bool {
	return cfg.Secrets.UsesOpenBao() && cfg.Profile == "prod"
}

// NeedsSeedSecrets returns true if the seed-secrets.sh script should be
// written into the bundle. This is true when the secrets backend is OpenBao
// (secrets.store: openbao) AND at least one header or env injection entry is
// configured. When store is "builtin" (or unset) this always returns false;
// seed-secrets.sh is only ever consumed by the seed-secrets container which is
// itself only emitted for the openbao store.
func NeedsSeedSecrets(cfg *config.Config) bool {
	if !cfg.Secrets.UsesOpenBao() {
		return false
	}
	return len(cfg.Secrets.Inject.Headers) > 0 || len(cfg.Secrets.Inject.Env) > 0
}

// NeedsSeedUsers returns true if a seed-users.sh script should be written into
// the bundle. This requires all three conditions to be true:
//   - auth.mode is "kratos"
//   - kratos.external is false (Kratos is managed locally)
//   - auth.seed_demo_users is true (opt-in; off by default)
//
// The script seeds demo Kratos identities on first boot and is only useful for
// demos or local testing. It is mounted into the seed-users init container
// defined in docker-compose.yml. Greenfield projects that do not set
// auth.seed_demo_users: true will not receive this script.
func NeedsSeedUsers(cfg *config.Config) bool {
	return cfg.Auth.Mode == config.AuthModeKratos &&
		!cfg.Kratos.External &&
		cfg.Auth.SeedDemoUsers
}
