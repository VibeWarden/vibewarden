// Package generate contains domain types for the stack generation subsystem.
package generate

// GeneratedCredentials holds the randomly generated credentials for a single
// `vibewarden generate` run. It is a value object — immutable after construction.
// The domain layer does not know how these are persisted or consumed.
type GeneratedCredentials struct {
	// PostgresPassword is the password for the Kratos Postgres database (32 chars).
	PostgresPassword string

	// KratosCookieSecret is the Kratos session cookie signing secret (32 chars).
	KratosCookieSecret string

	// KratosCipherSecret is the Kratos data encryption secret (32 chars).
	KratosCipherSecret string

	// GrafanaAdminPassword is the Grafana admin password (24 chars).
	GrafanaAdminPassword string

	// OpenBaoProdToken is the initial OpenBao root token written to .credentials at
	// bundle time. In dev mode OpenBao accepts it via BAO_DEV_ROOT_TOKEN_ID. In prod
	// mode this value is a placeholder — seed-secrets.sh replaces it with the real
	// root token produced by `bao operator init` on first boot and writes it back to
	// .credentials as OPENBAO_ROOT_TOKEN.
	//
	// Deprecated field name: previously named OpenBaoDevRootToken, previously stored
	// in .credentials as OPENBAO_DEV_ROOT_TOKEN. The env var was renamed to
	// OPENBAO_ROOT_TOKEN in v0.19 (issue #1345). The old name is still recognised in
	// .credentials files for one minor release (until v0.20) via a deprecation warning
	// emitted by `vibew bundle`.
	OpenBaoProdToken string
}
