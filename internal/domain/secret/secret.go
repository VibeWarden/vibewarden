// Package secret contains domain types for secret retrieval.
// It has zero external dependencies — only stdlib is permitted here.
package secret

// SecretSource indicates the origin of a retrieved secret.
//
//nolint:revive // SecretSource is the established public API type name used by secret retrieval adapters
type SecretSource string

const (
	// SourceOpenBao indicates the secret was retrieved from OpenBao.
	SourceOpenBao SecretSource = "openbao"

	// SourceCredentialsFile indicates the secret was retrieved from the .credentials file.
	SourceCredentialsFile SecretSource = "credentials_file"
)

// RetrievedSecret holds the key/value pairs retrieved from a secret path.
// It is a value object — immutable after construction.
type RetrievedSecret struct {
	// Path is the original path or resolved alias that was queried.
	Path string

	// Alias is the well-known alias if one was used, or empty string.
	Alias string

	// Data holds the key/value pairs of the secret.
	Data map[string]string

	// Source indicates where the secret was retrieved from.
	Source SecretSource
}
