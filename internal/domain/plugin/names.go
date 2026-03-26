// Package plugin contains domain types for the VibeWarden plugin system.
package plugin

import "errors"

// Name is a value object representing a unique plugin identifier.
// It is immutable and equality is determined by value.
type Name struct {
	value string
}

// NewName creates a validated Name value object.
// Returns an error if the name is empty.
func NewName(name string) (Name, error) {
	if name == "" {
		return Name{}, errors.New("plugin name cannot be empty")
	}
	return Name{value: name}, nil
}

// String returns the string representation of the plugin name.
func (n Name) String() string { return n.value }

// Well-known plugin name constants. These are the canonical identifiers
// used in vibewarden.yaml under the plugins: key.
const (
	NameTLS            = "tls"
	NameUserManagement = "user-management"
	NameRateLimiting   = "rate-limiting"
	NameGrafana        = "grafana"
	NameFleet          = "fleet"
)
