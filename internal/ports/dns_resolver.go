package ports

import "context"

// DNSResolver resolves host names to IP addresses. Implementations may use
// the system resolver, a custom net.Resolver, or a stub for testing.
//
// The interface exists to make DNS-dependent checks unit-testable without
// requiring real DNS queries or network access.
type DNSResolver interface {
	// LookupHost resolves host to a slice of addresses (IPv4 or IPv6 literals).
	// Returns an empty slice and no error when the host exists but has no records.
	// Returns a *net.DNSError with IsNotFound == true for NXDOMAIN responses.
	// Any other failure is returned as-is so callers can inspect the concrete type.
	LookupHost(ctx context.Context, host string) (addrs []string, err error)
}
