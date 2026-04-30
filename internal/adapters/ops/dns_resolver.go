package ops

import (
	"context"
	"net"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// dnsResolverTimeout is the per-lookup deadline applied by DNSResolverAdapter.
const dnsResolverTimeout = 3 * time.Second

// DNSResolverAdapter implements ports.DNSResolver using the system DNS resolver
// via net.Resolver. Each lookup is bound to a 3-second deadline so that DNS
// failures in "vibew doctor --preflight" never block the terminal for long.
type DNSResolverAdapter struct {
	resolver *net.Resolver
}

// NewDNSResolverAdapter creates a DNSResolverAdapter backed by the default
// system resolver. The returned adapter is safe for concurrent use.
func NewDNSResolverAdapter() *DNSResolverAdapter {
	return &DNSResolverAdapter{resolver: net.DefaultResolver}
}

// LookupHost resolves host using the system resolver with a 3-second timeout.
// It returns a *net.DNSError (IsNotFound == true) for NXDOMAIN responses, and
// propagates all other errors unchanged so callers can inspect the concrete type.
func (a *DNSResolverAdapter) LookupHost(ctx context.Context, host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsResolverTimeout)
	defer cancel()
	return a.resolver.LookupHost(ctx, host)
}

// Compile-time assertion that DNSResolverAdapter satisfies ports.DNSResolver.
var _ ports.DNSResolver = (*DNSResolverAdapter)(nil)
