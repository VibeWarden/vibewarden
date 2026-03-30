// Package ipfilter contains pure domain logic for IP-based access control.
// It has no external dependencies — only the Go standard library.
package ipfilter

import (
	"fmt"
	"net"
)

// Mode represents the IP filter operating mode.
type Mode string

const (
	// ModeAllowlist permits only requests from addresses in the configured list.
	// Any IP not in the list is blocked.
	ModeAllowlist Mode = "allowlist"

	// ModeBlocklist blocks requests from addresses in the configured list.
	// Any IP not in the list is permitted. This is the default/fallback mode.
	ModeBlocklist Mode = "blocklist"
)

// List is a parsed, immutable set of IP addresses and CIDR ranges used for
// matching. Construct one with ParseList; the zero value matches nothing.
type List struct {
	nets []*net.IPNet
	ips  []net.IP
}

// ParseList parses a slice of address strings (plain IPs or CIDR notation) into
// a List. It returns an error if any entry cannot be parsed as either form.
func ParseList(addresses []string) (List, error) {
	var l List
	for _, addr := range addresses {
		if _, ipNet, err := net.ParseCIDR(addr); err == nil {
			l.nets = append(l.nets, ipNet)
			continue
		}
		if ip := net.ParseIP(addr); ip != nil {
			l.ips = append(l.ips, ip)
			continue
		}
		return List{}, fmt.Errorf("ipfilter: %q is not a valid IP address or CIDR", addr)
	}
	return l, nil
}

// MatchesAny reports whether ip matches any entry in the list.
// A nil ip always returns false.
func (l List) MatchesAny(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, known := range l.ips {
		if known.Equal(ip) {
			return true
		}
	}
	for _, cidr := range l.nets {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsBlocked reports whether a request from ip should be blocked, given the
// filter mode.
//
//   - ModeAllowlist: the IP is blocked when it does NOT appear in the list.
//   - ModeBlocklist (and any unrecognised value): the IP is blocked when it
//     DOES appear in the list.
func IsBlocked(ip net.IP, list List, mode Mode) bool {
	matched := list.MatchesAny(ip)
	switch mode {
	case ModeAllowlist:
		return !matched
	default: // ModeBlocklist and any unrecognised value
		return matched
	}
}
