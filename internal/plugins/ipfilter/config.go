// Package ipfilter implements the VibeWarden IP filter plugin.
//
// It supports two mutually exclusive modes:
//
//   - allowlist: only requests from listed IPs/CIDRs are permitted; all others
//     receive 403 Forbidden before any further middleware runs.
//   - blocklist: requests from listed IPs/CIDRs are blocked with 403 Forbidden;
//     all other clients are allowed through.
//
// IP matching uses net.ParseIP for exact addresses and net.IPNet.Contains for
// CIDR ranges. IPv4-mapped IPv6 addresses (e.g. ::ffff:192.168.1.1) are handled
// transparently by the Go net package.
//
// The filter is applied before authentication so that blocked clients never
// trigger Kratos round-trips.
package ipfilter

// FilterMode selects the IP filter behaviour.
type FilterMode string

const (
	// FilterModeAllowlist permits only addresses in the list; all others are blocked.
	FilterModeAllowlist FilterMode = "allowlist"

	// FilterModeBlocklist blocks addresses in the list; all others are permitted.
	FilterModeBlocklist FilterMode = "blocklist"
)

// Config holds all settings for the IP filter plugin.
// It maps to the ip_filter section of vibewarden.yaml.
type Config struct {
	// Enabled toggles the IP filter plugin.
	Enabled bool

	// Mode selects the filter behaviour: "allowlist" or "blocklist".
	// Defaults to "blocklist" when empty.
	Mode FilterMode

	// Addresses is the list of IP addresses or CIDR ranges to match against.
	// Examples: "10.0.0.0/8", "192.168.1.100", "2001:db8::/32".
	Addresses []string

	// TrustProxyHeaders, when true, reads X-Forwarded-For to determine the
	// real client IP. Only enable when VibeWarden runs behind a trusted proxy.
	TrustProxyHeaders bool
}
