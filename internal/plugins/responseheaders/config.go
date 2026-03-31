// Package responseheaders implements the VibeWarden response-headers plugin.
//
// It allows operators to add, set (overwrite), and remove arbitrary HTTP
// response headers via config-driven rules in vibewarden.yaml.  The
// modifications are applied after all other middleware — including the
// security-headers plugin — so they can extend or override any header.
//
// Operations are applied in the order: remove → set → add.
package responseheaders

// Config holds all settings for the response-headers plugin.
// It maps to the response_headers section of vibewarden.yaml.
type Config struct {
	// Set maps header names to values that overwrite any existing value, or
	// create the header when it is not already present in the response.
	// Values may reference environment variables using the ${VAR} syntax;
	// Caddy resolves these at request time via its placeholder mechanism.
	Set map[string]string

	// Add maps header names to values that are appended to any existing value,
	// or create the header when it is not already present.
	// Values may reference environment variables using the ${VAR} syntax.
	Add map[string]string

	// Remove is the list of header names to delete from every response.
	// Header names are matched case-insensitively by Caddy.
	Remove []string
}
