package config

// UsesOpenBao reports whether the configured store backend is OpenBao.
// An empty Store defaults to "builtin", so OpenBao infrastructure is only
// provisioned when Store is explicitly "openbao". The deprecated Provider
// field is a separate axis (plugin runtime) and does not participate in
// this guard — compose store selection keys only off Store.
func (s SecretsConfig) UsesOpenBao() bool {
	return s.Enabled && s.Store == "openbao"
}

// SecretsConfig holds all settings for the secret management plugin.
// It maps to the secrets section of vibewarden.yaml.
type SecretsConfig struct {
	// Enabled toggles the secrets plugin (default: false).
	Enabled bool `mapstructure:"enabled"`

	// Store selects the secret store backend: "builtin" or "openbao".
	// Default: "builtin". When empty or unset, "builtin" is used.
	Store string `mapstructure:"store"`

	// Provider selects the secret store backend (default: "openbao").
	// Deprecated: use Store instead. Kept for backward compatibility.
	Provider string `mapstructure:"provider"`

	// Builtin holds settings for the built-in encrypted file store.
	Builtin SecretsBuiltinConfig `mapstructure:"builtin"`

	// OpenBao holds connection and authentication settings for the OpenBao server.
	OpenBao SecretsOpenBaoConfig `mapstructure:"openbao"`

	// Inject defines how fetched secrets are surfaced to the upstream app.
	Inject SecretsInjectConfig `mapstructure:"inject"`

	// Dynamic holds settings for dynamic credential generation.
	Dynamic SecretsDynamicConfig `mapstructure:"dynamic"`

	// Health holds settings for the secret health check subsystem.
	Health SecretsHealthConfig `mapstructure:"health"`

	// CacheTTL is how long fetched secrets are held in memory (default: "5m").
	CacheTTL string `mapstructure:"cache_ttl"`
}

// SecretsBuiltinConfig holds settings for the built-in encrypted file store.
type SecretsBuiltinConfig struct {
	// Path is the location of the encrypted secrets file.
	// Default: ".vibewarden/secrets.enc".
	Path string `mapstructure:"path"`

	// KeyFile is the path to a file containing the 32-byte hex-encoded master key.
	// If empty, the VIBEWARDEN_SECRETS_MASTER_KEY environment variable is used.
	KeyFile string `mapstructure:"key_file"`
}

// SecretsOpenBaoConfig holds connection settings for the OpenBao server.
type SecretsOpenBaoConfig struct {
	// Address is the OpenBao server URL (e.g. "http://openbao:8200").
	Address string `mapstructure:"address"`

	// Auth holds the authentication credentials.
	Auth SecretsOpenBaoAuthConfig `mapstructure:"auth"`

	// MountPath is the KV v2 mount path (default: "secret").
	MountPath string `mapstructure:"mount_path"`
}

// SecretsOpenBaoAuthConfig holds authentication credentials for OpenBao.
type SecretsOpenBaoAuthConfig struct {
	// Method is the auth method: "token" or "approle".
	Method string `mapstructure:"method"`

	// Token is the static token. Used when Method is "token".
	Token string `mapstructure:"token"`

	// RoleID is the AppRole role_id. Used when Method is "approle".
	RoleID string `mapstructure:"role_id"`

	// SecretID is the AppRole secret_id. Used when Method is "approle".
	SecretID string `mapstructure:"secret_id"`
}

// SecretsInjectConfig defines how secrets are injected into proxied requests.
type SecretsInjectConfig struct {
	// Headers is the list of secrets to inject as HTTP request headers.
	Headers []SecretsHeaderInjection `mapstructure:"headers"`

	// EnvFile is the path to write a .env file containing secret values.
	EnvFile string `mapstructure:"env_file"`

	// Env is the list of secrets to write into the env file.
	Env []SecretsEnvInjection `mapstructure:"env"`
}

// SecretsHeaderInjection maps a secret key to an HTTP request header name.
type SecretsHeaderInjection struct {
	// SecretPath is the KV path of the secret.
	SecretPath string `mapstructure:"secret_path"`

	// SecretKey is the key within the secret map.
	SecretKey string `mapstructure:"secret_key"`

	// Header is the HTTP header name to set.
	Header string `mapstructure:"header"`

	// ValueTemplate is an optional template string containing ${secret://path/key}
	// placeholders. When set, the resolved template is used as the header value
	// instead of the raw secret. When empty, the secret value is used directly.
	ValueTemplate string `mapstructure:"value_template"`
}

// SecretsEnvInjection maps a secret key to an environment variable name.
type SecretsEnvInjection struct {
	// SecretPath is the KV path of the secret.
	SecretPath string `mapstructure:"secret_path"`

	// SecretKey is the key within the secret map.
	SecretKey string `mapstructure:"secret_key"`

	// EnvVar is the environment variable name to write in the env file.
	EnvVar string `mapstructure:"env_var"`

	// ValueTemplate is an optional template string containing ${secret://path/key}
	// placeholders. When set, the resolved template is used as the env var value
	// instead of the raw secret. When empty, the secret value is used directly.
	ValueTemplate string `mapstructure:"value_template"`
}

// SecretsDynamicConfig holds settings for dynamic credential generation.
type SecretsDynamicConfig struct {
	// Postgres configures the OpenBao database engine for Postgres dynamic creds.
	Postgres SecretsDynamicPostgresConfig `mapstructure:"postgres"`
}

// SecretsDynamicPostgresConfig holds settings for dynamic Postgres credential generation.
type SecretsDynamicPostgresConfig struct {
	// Enabled toggles dynamic Postgres credential generation.
	Enabled bool `mapstructure:"enabled"`

	// Roles is the list of OpenBao database roles to request credentials for.
	Roles []SecretsDynamicRole `mapstructure:"roles"`
}

// SecretsDynamicRole defines a single OpenBao database role and where to inject its credentials.
type SecretsDynamicRole struct {
	// Name is the OpenBao database role name.
	Name string `mapstructure:"name"`

	// EnvVarUser is the env var name to write the generated username into.
	EnvVarUser string `mapstructure:"env_var_user"`

	// EnvVarPassword is the env var name to write the generated password into.
	EnvVarPassword string `mapstructure:"env_var_password"`
}

// SecretsHealthConfig holds settings for the secret health check subsystem.
type SecretsHealthConfig struct {
	// CheckInterval is how often to run health checks (default: "6h").
	CheckInterval string `mapstructure:"check_interval"`

	// MaxStaticAge is the maximum acceptable age of a static secret (default: "2160h").
	MaxStaticAge string `mapstructure:"max_static_age"`

	// WeakPatterns is the list of substrings that indicate a weak/default secret.
	WeakPatterns []string `mapstructure:"weak_patterns"`
}
