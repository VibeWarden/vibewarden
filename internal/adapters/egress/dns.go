package egress

import (
	"context"
	"fmt"
	"net"
)

// privateRanges contains the IP networks that are considered private, reserved,
// or otherwise forbidden as egress targets when SSRF protection is active.
// Covered ranges:
//   - 127.0.0.0/8     — IPv4 loopback (RFC 5735)
//   - 10.0.0.0/8      — RFC 1918 private
//   - 172.16.0.0/12   — RFC 1918 private
//   - 192.168.0.0/16  — RFC 1918 private
//   - 169.254.0.0/16  — IPv4 link-local (RFC 3927)
//   - 100.64.0.0/10   — shared address space (RFC 6598)
//   - 0.0.0.0/8       — "this" network (RFC 1122)
//   - 192.0.0.0/24    — IETF protocol assignments (RFC 6890)
//   - 192.0.2.0/24    — documentation (TEST-NET-1, RFC 5737)
//   - 198.51.100.0/24 — documentation (TEST-NET-2, RFC 5737)
//   - 203.0.113.0/24  — documentation (TEST-NET-3, RFC 5737)
//   - 224.0.0.0/4     — IPv4 multicast (RFC 3171)
//   - 240.0.0.0/4     — reserved (RFC 1112)
//   - 255.255.255.255/32 — broadcast
//   - ::1/128         — IPv6 loopback
//   - fc00::/7        — IPv6 unique local (RFC 4193)
//   - fe80::/10       — IPv6 link-local (RFC 4291)
//   - ff00::/8        — IPv6 multicast (RFC 4291)
var privateRanges []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"0.0.0.0/8",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4", // IPv4 multicast (RFC 3171)
		"240.0.0.0/4",
		"255.255.255.255/32",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// These are hardcoded literals; this branch is unreachable in practice.
			panic(fmt.Sprintf("egress: invalid built-in CIDR %q: %v", cidr, err))
		}
		privateRanges = append(privateRanges, ipNet)
	}
}

// SSRFGuardConfig holds the configuration for the SSRF guard.
type SSRFGuardConfig struct {
	// BlockPrivate enables blocking of private and reserved IP ranges.
	// Corresponds to egress.dns.block_private (default: true).
	BlockPrivate bool

	// AllowedPrivate is an optional list of CIDR ranges that are exempt from
	// the private-IP block. These are parsed once at construction time.
	// Corresponds to egress.dns.allowed_private.
	AllowedPrivate []string
}

// SSRFGuard wraps a net.Resolver and enforces SSRF protection by rejecting
// dial attempts that resolve to private or reserved IP addresses.
type SSRFGuard struct {
	cfg            SSRFGuardConfig
	resolver       *net.Resolver
	allowedPrivate []*net.IPNet
}

// NewSSRFGuard creates a new SSRFGuard from the given configuration.
// It returns an error if any CIDR in AllowedPrivate is malformed.
func NewSSRFGuard(cfg SSRFGuardConfig) (*SSRFGuard, error) {
	allowed := make([]*net.IPNet, 0, len(cfg.AllowedPrivate))
	for _, cidr := range cfg.AllowedPrivate {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("egress: allowed_private CIDR %q is invalid: %w", cidr, err)
		}
		allowed = append(allowed, ipNet)
	}
	return &SSRFGuard{
		cfg:            cfg,
		resolver:       net.DefaultResolver,
		allowedPrivate: allowed,
	}, nil
}

// DialContext is a net.Dialer-compatible function that performs SSRF protection
// before establishing a TCP connection. It resolves the target host, checks all
// resolved IP addresses against the private ranges, and returns ErrSSRFBlocked
// if any resolved address falls within a blocked range.
//
// This method is intended to be used as the DialContext field of an
// http.Transport.
func (g *SSRFGuard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("egress: parsing address %q: %w", addr, err)
	}

	if g.cfg.BlockPrivate {
		if err := g.checkHost(ctx, host); err != nil {
			return nil, err
		}
	}

	// Proceed with the actual dial using the standard dialer.
	d := &net.Dialer{}
	return d.DialContext(ctx, network, net.JoinHostPort(host, port))
}

// checkHost resolves host and verifies that none of the resolved addresses fall
// within a blocked private/reserved range. It returns ErrSSRFBlocked if any
// resolved address is blocked, nil otherwise.
func (g *SSRFGuard) checkHost(ctx context.Context, host string) error {
	// If the host is already an IP literal, check it directly without DNS lookup.
	if ip := net.ParseIP(host); ip != nil {
		if g.isBlocked(ip) {
			return &SSRFBlockedError{Host: host, IP: ip}
		}
		return nil
	}

	addrs, err := g.resolver.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("egress: resolving %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if g.isBlocked(ip) {
			return &SSRFBlockedError{Host: host, IP: ip}
		}
	}
	return nil
}

// isBlocked returns true if ip falls within a blocked private range and is not
// covered by an allowedPrivate exemption.
func (g *SSRFGuard) isBlocked(ip net.IP) bool {
	blocked := false
	for _, r := range privateRanges {
		if r.Contains(ip) {
			blocked = true
			break
		}
	}
	if !blocked {
		return false
	}
	// Check exemptions.
	for _, allowed := range g.allowedPrivate {
		if allowed.Contains(ip) {
			return false
		}
	}
	return true
}

// SSRFBlockedError is returned when an outbound request is blocked because the
// target hostname resolves to a private or reserved IP address.
type SSRFBlockedError struct {
	// Host is the hostname that triggered the block.
	Host string
	// IP is the resolved IP address that matched a blocked range.
	IP net.IP
}

// Error implements the error interface.
func (e *SSRFBlockedError) Error() string {
	return fmt.Sprintf("egress: SSRF protection blocked request to %q (resolved to %s)", e.Host, e.IP)
}
