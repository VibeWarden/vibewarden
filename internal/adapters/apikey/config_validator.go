// Package apikey provides an implementation of the ports.APIKeyValidator
// interface backed by a static list of keys defined in vibewarden.yaml.
//
// Keys are stored as SHA-256 hex hashes in configuration; plaintext keys are
// never persisted. The Validate method uses constant-time comparison via the
// domain's auth.APIKey.Matches helper, which internally uses crypto/subtle.
package apikey

import (
	"context"
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ConfigValidator is an implementation of ports.APIKeyValidator that validates
// API keys against a static list loaded from the vibewarden.yaml configuration.
//
// This is the v1 implementation; future versions may use a database-backed
// store instead.
type ConfigValidator struct {
	keys []*auth.APIKey
}

// NewConfigValidator builds a ConfigValidator from the given list of key
// entries. It converts each entry into an auth.APIKey entity. Entries with an
// empty name or empty hash are rejected with a descriptive error.
func NewConfigValidator(entries []config.APIKeyEntry) (*ConfigValidator, error) {
	keys := make([]*auth.APIKey, 0, len(entries))
	for i, e := range entries {
		if e.Name == "" {
			return nil, fmt.Errorf("api key entry %d: name cannot be empty", i)
		}
		if e.Hash == "" {
			return nil, fmt.Errorf("api key entry %q: hash cannot be empty", e.Name)
		}

		scopes := make([]auth.Scope, len(e.Scopes))
		for j, s := range e.Scopes {
			scopes[j] = auth.Scope(s)
		}

		k := &auth.APIKey{
			Name:      e.Name,
			KeyHash:   e.Hash,
			Scopes:    scopes,
			Active:    true,
			CreatedAt: time.Time{}, // unknown for config-defined keys
		}
		if err := k.Validate(); err != nil {
			return nil, fmt.Errorf("api key entry %q: %w", e.Name, err)
		}
		keys = append(keys, k)
	}
	return &ConfigValidator{keys: keys}, nil
}

// Validate iterates over the registered keys and returns the first matching
// active key. The plaintext key is compared using constant-time hashing via
// auth.APIKey.Matches. Returns ports.ErrAPIKeyInvalid when no match is found
// or the matching key is inactive.
func (v *ConfigValidator) Validate(_ context.Context, plaintextKey string) (*auth.APIKey, error) {
	for _, k := range v.keys {
		if k.Matches(plaintextKey) {
			if !k.Active {
				return nil, ports.ErrAPIKeyInvalid
			}
			return k, nil
		}
	}
	return nil, ports.ErrAPIKeyInvalid
}
