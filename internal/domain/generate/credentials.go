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

	// OpenBaoDevRootToken is the OpenBao dev mode root token (32 chars).
	OpenBaoDevRootToken string
}
