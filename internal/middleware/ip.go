// Package middleware provides HTTP middleware for VibeWarden.
package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ExtractClientIP extracts the client IP address from an HTTP request.
//
// When trustProxy is true and the X-Forwarded-For header is present, the
// leftmost (original client) IP in the chain is returned. This is the correct
// behavior when VibeWarden runs behind a trusted load balancer or reverse proxy.
//
// When trustProxy is false (the default), the IP is taken directly from
// r.RemoteAddr, which cannot be spoofed by the client.
//
// The returned string is always an IP address without a port number.
// IPv6 addresses are returned in their canonical form.
// If no valid IP can be extracted, the empty string is returned.
func ExtractClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For can contain a comma-separated list of IPs.
			// The leftmost entry is the original client; entries to the right
			// are appended by successive proxies.
			first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if ip := NormalizeIP(first); ip != "" {
				return ip
			}
			// Fall through to RemoteAddr if the header value is not a valid IP.
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may already be a bare IP (no port) in some test scenarios.
		return NormalizeIP(r.RemoteAddr)
	}
	return NormalizeIP(host)
}

// NormalizeIP parses and normalizes an IP address string.
//
// For IPv4 addresses it returns the standard dotted-decimal notation.
// For IPv6 addresses it returns the canonical RFC 5952 representation.
// If the string is not a valid IP address, the empty string is returned.
func NormalizeIP(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ""
	}
	// net.IP.String() returns the canonical form for both IPv4 and IPv6.
	return parsed.String()
}
