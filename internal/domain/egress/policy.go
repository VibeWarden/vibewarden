// Package egress contains the domain model for the egress proxy plugin.
// This package has zero external dependencies — only the Go standard library.
//
// The egress proxy intercepts outbound API calls made by the wrapped application
// and applies allowlisting, secret injection, rate limiting, circuit breaking,
// and structured logging before forwarding requests to external services.
package egress

import "errors"

// Policy determines the default disposition for egress traffic that does not
// match any configured route.
type Policy string

const (
	// PolicyAllow permits egress traffic that does not match any route.
	// Use with caution — prefer PolicyDeny for production deployments.
	PolicyAllow Policy = "allow"

	// PolicyDeny blocks egress traffic that does not match any route.
	// This is the recommended default for production deployments.
	PolicyDeny Policy = "deny"
)

// Validate returns an error when the policy value is not recognised.
func (p Policy) Validate() error {
	switch p {
	case PolicyAllow, PolicyDeny:
		return nil
	case "":
		return errors.New("egress policy cannot be empty")
	default:
		return errors.New("egress policy must be \"allow\" or \"deny\"")
	}
}

// String returns the string representation of the policy.
func (p Policy) String() string { return string(p) }
