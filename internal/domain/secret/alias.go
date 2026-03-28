package secret

// WellKnownAlias maps user-friendly names to OpenBao paths and credential file keys.
// It is a value object — immutable after construction.
type WellKnownAlias struct {
	// Name is the alias (e.g. "postgres", "kratos").
	Name string

	// OpenBaoPath is the static KV path in OpenBao (e.g. "infra/postgres").
	// Empty string means this alias has no static OpenBao path.
	OpenBaoPath string

	// DynamicRole is the database secret engine role name for dynamic credentials.
	// Empty string if this alias does not support dynamic credentials.
	DynamicRole string

	// CredentialsFileKeys maps .credentials file keys to output field names.
	// e.g. {"POSTGRES_PASSWORD": "password"}
	CredentialsFileKeys map[string]string

	// EnvPrefix is the prefix for --env output (e.g. "POSTGRES_").
	EnvPrefix string
}

// wellKnownAliases is the registry of all supported well-known aliases.
var wellKnownAliases = []WellKnownAlias{
	{
		Name:        "postgres",
		OpenBaoPath: "infra/postgres",
		DynamicRole: "app-readwrite",
		CredentialsFileKeys: map[string]string{
			"POSTGRES_PASSWORD": "password",
		},
		EnvPrefix: "POSTGRES_",
	},
	{
		Name:        "kratos",
		OpenBaoPath: "infra/kratos",
		DynamicRole: "",
		CredentialsFileKeys: map[string]string{
			"KRATOS_SECRETS_COOKIE": "cookie_secret",
			"KRATOS_SECRETS_CIPHER": "cipher_secret",
		},
		EnvPrefix: "KRATOS_",
	},
	{
		Name:        "grafana",
		OpenBaoPath: "infra/grafana",
		DynamicRole: "",
		CredentialsFileKeys: map[string]string{
			"GRAFANA_ADMIN_PASSWORD": "admin_password",
		},
		EnvPrefix: "GRAFANA_",
	},
	{
		Name:        "openbao",
		OpenBaoPath: "", // root token is not stored in OpenBao itself
		DynamicRole: "",
		CredentialsFileKeys: map[string]string{
			"OPENBAO_DEV_ROOT_TOKEN": "dev_root_token",
		},
		EnvPrefix: "OPENBAO_",
	},
}

// ResolveAlias returns the WellKnownAlias for the given name, or nil if not found.
// The lookup is case-sensitive and exact.
func ResolveAlias(name string) *WellKnownAlias {
	for i := range wellKnownAliases {
		if wellKnownAliases[i].Name == name {
			return &wellKnownAliases[i]
		}
	}
	return nil
}

// ListAliases returns all well-known aliases. The returned slice is a copy —
// callers may not modify it to affect the registry.
func ListAliases() []WellKnownAlias {
	out := make([]WellKnownAlias, len(wellKnownAliases))
	copy(out, wellKnownAliases)
	return out
}
