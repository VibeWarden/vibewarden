// Package presets embeds the built-in Kratos identity schema presets and
// provides a function to resolve a preset name to its JSON content.
package presets

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed email_password.json
var emailPassword []byte

//go:embed email_only.json
var emailOnly []byte

//go:embed username_password.json
var usernamePassword []byte

//go:embed social.json
var social []byte

// Known preset names.
const (
	// PresetEmailPassword is the preset for email + password authentication.
	PresetEmailPassword = "email_password"
	// PresetEmailOnly is the preset for email-only (magic link / code) authentication.
	PresetEmailOnly = "email_only"
	// PresetUsernamePassword is the preset for username + password authentication.
	PresetUsernamePassword = "username_password"
	// PresetSocial is the preset for social login (OAuth2/OIDC) combined with
	// email + password. It extends email_password with name and picture traits
	// populated by OIDC mappers.
	PresetSocial = "social"
)

// Resolve returns the identity schema JSON for the given name.
// name may be one of the built-in preset names (PresetEmailPassword,
// PresetEmailOnly, PresetUsernamePassword, PresetSocial) or a filesystem
// path to a custom JSON schema file.
// Returns an error when name is empty, unknown, or the custom file cannot
// be read.
func Resolve(name string) ([]byte, error) {
	switch name {
	case PresetEmailPassword:
		return emailPassword, nil
	case PresetEmailOnly:
		return emailOnly, nil
	case PresetUsernamePassword:
		return usernamePassword, nil
	case PresetSocial:
		return social, nil
	case "":
		return nil, fmt.Errorf("identity schema name must not be empty")
	default:
		// Treat as a filesystem path to a custom schema.
		data, err := os.ReadFile(name) //nolint:gosec // name is a file path from operator config for a custom identity schema, not user input
		if err != nil {
			return nil, fmt.Errorf("reading custom identity schema %q: %w", name, err)
		}
		return data, nil
	}
}

// IsPreset reports whether name is one of the built-in preset names.
func IsPreset(name string) bool {
	switch name {
	case PresetEmailPassword, PresetEmailOnly, PresetUsernamePassword, PresetSocial:
		return true
	default:
		return false
	}
}
