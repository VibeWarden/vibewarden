package secret

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// secretURIPrefix is the scheme prefix that identifies a secret URI in config
// string fields.
const secretURIPrefix = "secret://"

// URI is a value object representing a parsed secret:// URI.
// The URI format is secret://path/key where the last segment is the key and
// everything before it is the path.
//
// Example: secret://auth/google/client_id
//
//	path = "auth/google"
//	key  = "client_id"
type URI struct {
	path string
	key  string
}

// ParseURI parses a secret:// URI string into a URI value object.
// The URI must have the format secret://path/key where path contains at least
// one segment and key is the final segment.
//
// Returns an error when the URI is malformed, has no path, or has no key.
// Additionally rejects:
//   - any percent character ('%') — percent-encoding (e.g. %2F, or the
//     double-encoded %252F which decodes back to %2F) is never needed in a
//     legitimate secret path and is the primary way to smuggle a path separator
//     past segment splitting in downstream HTTP clients
//   - empty path segments, including a leading "/" (absolute path)
//   - ".." segments — path traversal (OWASP A03)
func ParseURI(raw string) (URI, error) {
	if !strings.HasPrefix(raw, secretURIPrefix) {
		return URI{}, fmt.Errorf("secret URI must start with %q, got %q", secretURIPrefix, raw)
	}

	body := strings.TrimPrefix(raw, secretURIPrefix)
	if body == "" {
		return URI{}, errors.New("secret URI is empty after scheme: expected secret://path/key")
	}

	// Reject any percent character. Percent-encoding is decoded by HTTP clients
	// into raw bytes (e.g. %2F -> '/'), bypassing segment-based validation
	// downstream (e.g. in the OpenBao adapter, which builds HTTP API paths from
	// the URI path). Rejecting '%' outright also defeats double-encoding such as
	// %252F, which decodes to %2F and then to '/'. Legitimate secret paths never
	// require percent-encoding — use a literal '/' separator.
	if strings.Contains(body, "%") {
		return URI{}, fmt.Errorf("secret URI %q contains a percent character; percent-encoding is not allowed, use literal '/' separators", raw)
	}

	// Validate every segment before extracting path and key.
	// An empty segment indicates a leading '/', trailing '/', or consecutive '//'.
	// A ".." segment is a path traversal attempt that could reach unintended
	// OpenBao API paths (e.g. secret://../sys/mounts/secret/key).
	for _, seg := range strings.Split(body, "/") {
		switch seg {
		case "":
			return URI{}, fmt.Errorf("secret URI %q contains an empty path segment (leading slash, trailing slash, or consecutive slashes)", raw)
		case "..":
			return URI{}, fmt.Errorf("secret URI %q contains a path traversal segment \"..\"", raw)
		}
	}

	// The last segment is the key, everything before it is the path.
	idx := strings.LastIndex(body, "/")
	if idx < 0 {
		return URI{}, fmt.Errorf("secret URI %q must contain at least one '/' separating path and key", raw)
	}

	path := body[:idx]
	key := body[idx+1:]

	if path == "" {
		return URI{}, fmt.Errorf("secret URI %q has an empty path", raw)
	}
	if key == "" {
		return URI{}, fmt.Errorf("secret URI %q has an empty key", raw)
	}

	return URI{path: path, key: key}, nil
}

// IsURI reports whether the given string is a secret:// URI.
func IsURI(s string) bool {
	return strings.HasPrefix(s, secretURIPrefix)
}

// Path returns the store path portion of the URI.
// For secret://auth/google/client_id this returns "auth/google".
func (u URI) Path() string { return u.path }

// Key returns the key portion of the URI.
// For secret://auth/google/client_id this returns "client_id".
func (u URI) Key() string { return u.key }

// String returns the canonical string representation of the URI.
func (u URI) String() string {
	return secretURIPrefix + u.path + "/" + u.key
}

// placeholderRe matches ${secret://...} placeholders that are NOT preceded by
// an extra $. A negative lookbehind is not available in Go's RE2, so
// FindPlaceholders filters escaped matches manually.
//
// The regex captures the inner secret:// URI (everything between ${ and }).
var placeholderRe = regexp.MustCompile(`\$\{(secret://[^}]+)\}`)

// escapedPlaceholderRe matches $${secret://...} — escaped placeholders that
// should be converted to their literal ${secret://...} form.
var escapedPlaceholderRe = regexp.MustCompile(`\$\$\{(secret://[^}]+)\}`)

// Placeholder represents a single ${secret://path/key} occurrence in a string.
type Placeholder struct {
	// Raw is the full matched text including the ${} delimiters.
	Raw string

	// URI is the parsed secret URI extracted from the placeholder.
	URI URI
}

// ContainsPlaceholder reports whether s contains at least one
// ${secret://...} placeholder that is not escaped with $${...}.
func ContainsPlaceholder(s string) bool {
	return len(FindPlaceholders(s)) > 0
}

// FindPlaceholders extracts all ${secret://...} placeholders from s.
// Escaped placeholders ($${secret://...}) are not included in the result.
// Returns nil when no unescaped placeholders are found.
func FindPlaceholders(s string) []Placeholder {
	matches := placeholderRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return nil
	}

	var result []Placeholder
	for _, loc := range matches {
		// loc[0] is the start of the full match (${secret://...})
		// Check whether it is preceded by an extra $ (escaped).
		if loc[0] > 0 && s[loc[0]-1] == '$' {
			continue
		}

		raw := s[loc[0]:loc[1]]
		inner := s[loc[2]:loc[3]]

		uri, err := ParseURI(inner)
		if err != nil {
			// Malformed URI inside placeholder — skip silently so callers
			// can validate later or report the error.
			continue
		}

		result = append(result, Placeholder{Raw: raw, URI: uri})
	}

	return result
}

// ContainsEscapedPlaceholder reports whether s contains at least one
// $${secret://...} escaped placeholder.
func ContainsEscapedPlaceholder(s string) bool {
	return escapedPlaceholderRe.MatchString(s)
}

// UnescapePlaceholders converts all $${secret://...} occurrences in s to
// their literal ${secret://...} form. This allows users to include literal
// placeholder text in config values without triggering resolution.
func UnescapePlaceholders(s string) string {
	return escapedPlaceholderRe.ReplaceAllString(s, `${$1}`)
}
