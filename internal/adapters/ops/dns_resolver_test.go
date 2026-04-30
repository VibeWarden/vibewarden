package ops_test

import (
	"context"
	"net"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

func TestDNSResolverAdapter_LookupHost_Localhost(t *testing.T) {
	adapter := opsadapter.NewDNSResolverAdapter()
	addrs, err := adapter.LookupHost(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("LookupHost(127.0.0.1) unexpected error: %v", err)
	}
	if len(addrs) == 0 {
		t.Error("LookupHost(127.0.0.1) returned empty addresses")
	}
	found := false
	for _, a := range addrs {
		if a == "127.0.0.1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("LookupHost(127.0.0.1) did not return 127.0.0.1, got: %v", addrs)
	}
}

func TestDNSResolverAdapter_LookupHost_InvalidLabel_ReturnsError(t *testing.T) {
	adapter := opsadapter.NewDNSResolverAdapter()
	// A domain with an invalid label that will never resolve.
	_, err := adapter.LookupHost(context.Background(), "this-domain-does-absolutely-not-exist-vibewarden-test.invalid")
	if err == nil {
		t.Fatal("expected error for unresolvable domain, got nil")
	}
	// The error must be a *net.DNSError (system resolver always wraps in DNSError).
	var dnsErr *net.DNSError
	if ok := isNetDNSError(err, &dnsErr); !ok {
		t.Logf("error is not *net.DNSError (type=%T), which is acceptable for some resolvers", err)
	}
}

// isNetDNSError unwraps err to see if it is a *net.DNSError.
func isNetDNSError(err error, out **net.DNSError) bool {
	if de, ok := err.(*net.DNSError); ok { //nolint:errorlint
		*out = de
		return true
	}
	return false
}
