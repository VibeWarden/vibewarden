package ports

import (
	"context"
	"errors"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
)

// ErrAPIKeyInvalid is returned by APIKeyValidator.Validate when the presented
// key does not match any registered key, or when the matching key is inactive.
var ErrAPIKeyInvalid = errors.New("api key invalid or inactive")

// APIKeyValidator is the outbound port for validating API keys.
// Implementations look up the key in a backing store (e.g. config file, DB)
// and return the matching APIKey entity on success.
type APIKeyValidator interface {
	// Validate looks up the plaintext key and returns the matching APIKey on
	// success. The implementation is responsible for constant-time comparison
	// and must return ErrAPIKeyInvalid when the key is unknown or inactive.
	Validate(ctx context.Context, plaintextKey string) (*auth.APIKey, error)
}

// APIKeyConfig holds configuration for the API key middleware.
type APIKeyConfig struct {
	// Header is the request header from which the API key is extracted.
	// Defaults to "X-API-Key" when empty.
	Header string

	// ScopeRules is an ordered list of path+method authorization rules applied
	// after successful key validation. The first matching rule determines the
	// required scopes. When no rule matches, the request is allowed (open by
	// default). Rules must contain valid path.Match patterns; invalid patterns
	// are silently skipped during matching.
	ScopeRules []auth.ScopeRule
}
