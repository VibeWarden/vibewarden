package secret

import (
	"errors"
	"fmt"
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
func ParseURI(raw string) (URI, error) {
	if !strings.HasPrefix(raw, secretURIPrefix) {
		return URI{}, fmt.Errorf("secret URI must start with %q, got %q", secretURIPrefix, raw)
	}

	body := strings.TrimPrefix(raw, secretURIPrefix)
	if body == "" {
		return URI{}, errors.New("secret URI is empty after scheme: expected secret://path/key")
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
