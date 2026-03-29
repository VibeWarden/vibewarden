package egress_test

import (
	"context"
	"errors"
	"net"
	"testing"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
)

// TestNewSSRFGuard_InvalidAllowedPrivate verifies that malformed CIDRs in
// AllowedPrivate are rejected at construction time.
func TestNewSSRFGuard_InvalidAllowedPrivate(t *testing.T) {
	_, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate:   true,
		AllowedPrivate: []string{"not-a-cidr"},
	})
	if err == nil {
		t.Fatal("NewSSRFGuard should have returned an error for malformed CIDR")
	}
}

// TestNewSSRFGuard_ValidAllowedPrivate verifies that valid CIDRs are accepted.
func TestNewSSRFGuard_ValidAllowedPrivate(t *testing.T) {
	_, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate:   true,
		AllowedPrivate: []string{"192.168.1.0/24", "10.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard returned unexpected error: %v", err)
	}
}

// TestSSRFGuard_IsBlocked_IPLiteral exercises DialContext with IP literal addresses
// directly in the addr argument, bypassing DNS resolution.
func TestSSRFGuard_IsBlocked_IPLiteral(t *testing.T) {
	tests := []struct {
		name        string
		addr        string
		allowedCIDR []string
		wantBlocked bool
	}{
		{
			name:        "loopback 127.0.0.1 is blocked",
			addr:        "127.0.0.1:80",
			wantBlocked: true,
		},
		{
			name:        "RFC 1918 10.x is blocked",
			addr:        "10.0.0.1:80",
			wantBlocked: true,
		},
		{
			name:        "RFC 1918 172.16.x is blocked",
			addr:        "172.16.0.1:80",
			wantBlocked: true,
		},
		{
			name:        "RFC 1918 192.168.x is blocked",
			addr:        "192.168.1.100:80",
			wantBlocked: true,
		},
		{
			name:        "link-local 169.254.x is blocked",
			addr:        "169.254.1.1:80",
			wantBlocked: true,
		},
		{
			name:        "IPv6 loopback ::1 is blocked",
			addr:        "[::1]:80",
			wantBlocked: true,
		},
		{
			name:        "IPv6 link-local fe80:: is blocked",
			addr:        "[fe80::1]:80",
			wantBlocked: true,
		},
		{
			name:        "IPv6 unique local fc00:: is blocked",
			addr:        "[fc00::1]:80",
			wantBlocked: true,
		},
		{
			name:        "public IP is not blocked",
			addr:        "1.1.1.1:80",
			wantBlocked: false,
		},
		{
			name:        "public IP 8.8.8.8 is not blocked",
			addr:        "8.8.8.8:80",
			wantBlocked: false,
		},
		{
			name:        "192.168.1.1 blocked but exempted by allowed_private",
			addr:        "192.168.1.1:80",
			allowedCIDR: []string{"192.168.1.0/24"},
			wantBlocked: false,
		},
		{
			name:        "10.0.0.1 blocked and not in allowed_private",
			addr:        "10.0.0.1:80",
			allowedCIDR: []string{"192.168.1.0/24"},
			wantBlocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
				BlockPrivate:   true,
				AllowedPrivate: tt.allowedCIDR,
			})
			if err != nil {
				t.Fatalf("NewSSRFGuard: %v", err)
			}

			_, dialErr := guard.DialContext(context.Background(), "tcp", tt.addr)
			gotBlocked := isSSRFBlocked(dialErr)

			if gotBlocked != tt.wantBlocked {
				t.Errorf("DialContext(%q): blocked=%v, want blocked=%v (err=%v)",
					tt.addr, gotBlocked, tt.wantBlocked, dialErr)
			}
		})
	}
}

// TestSSRFGuard_BlockPrivateFalse verifies that when BlockPrivate is false,
// private IPs are allowed through without checking.
func TestSSRFGuard_BlockPrivateFalse(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: false,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	// When BlockPrivate is false, the guard should not reject private IPs.
	// We can't actually dial loopback in a unit test reliably, but we can check
	// that the error returned is NOT an SSRFBlockedError (it may be a connection
	// refused error or similar).
	_, dialErr := guard.DialContext(context.Background(), "tcp", "127.0.0.1:1")
	if isSSRFBlocked(dialErr) {
		t.Error("DialContext should not return SSRFBlockedError when BlockPrivate is false")
	}
}

// TestSSRFGuard_PublicDomain exercises DialContext with a real resolvable public
// hostname. This test is skipped when DNS is unavailable.
func TestSSRFGuard_PublicDomain(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: true,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	// Attempt to dial a well-known public host. We expect either a successful
	// connection (which we immediately close) or a network-level error (e.g.
	// connection refused). What we must NOT get is SSRFBlockedError.
	conn, dialErr := guard.DialContext(context.Background(), "tcp", "1.1.1.1:443")
	if conn != nil {
		conn.Close() //nolint:errcheck
	}
	if isSSRFBlocked(dialErr) {
		t.Errorf("DialContext to public IP should not be blocked, got: %v", dialErr)
	}
}

// TestSSRFBlockedError_Error verifies the error message format.
func TestSSRFBlockedError_Error(t *testing.T) {
	e := &egressadapter.SSRFBlockedError{
		Host: "internal.corp.example",
		IP:   net.ParseIP("10.0.0.1"),
	}
	msg := e.Error()
	if msg == "" {
		t.Error("SSRFBlockedError.Error() returned empty string")
	}
	for _, want := range []string{"SSRF", "internal.corp.example", "10.0.0.1"} {
		if !contains(msg, want) {
			t.Errorf("SSRFBlockedError.Error() = %q, does not contain %q", msg, want)
		}
	}
}

// TestSSRFGuard_Multicast verifies that multicast addresses are blocked.
func TestSSRFGuard_Multicast(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: true,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	multicastAddrs := []string{
		"224.0.0.1:80", // IPv4 multicast
		"239.0.0.1:80", // IPv4 multicast (admin-scoped)
		"[ff02::1]:80", // IPv6 multicast
	}
	for _, addr := range multicastAddrs {
		t.Run(addr, func(t *testing.T) {
			_, dialErr := guard.DialContext(context.Background(), "tcp", addr)
			if !isSSRFBlocked(dialErr) {
				t.Errorf("DialContext(%q): expected SSRFBlockedError, got %v", addr, dialErr)
			}
		})
	}
}

// TestSSRFGuard_SharedAddressSpace verifies that the RFC 6598 shared address
// space (100.64.0.0/10, used by carrier-grade NAT) is blocked.
func TestSSRFGuard_SharedAddressSpace(t *testing.T) {
	guard, err := egressadapter.NewSSRFGuard(egressadapter.SSRFGuardConfig{
		BlockPrivate: true,
	})
	if err != nil {
		t.Fatalf("NewSSRFGuard: %v", err)
	}

	_, dialErr := guard.DialContext(context.Background(), "tcp", "100.64.0.1:80")
	if !isSSRFBlocked(dialErr) {
		t.Errorf("DialContext(100.64.0.1:80): expected SSRFBlockedError, got %v", dialErr)
	}
}

// isSSRFBlocked returns true if err is (or wraps) an *SSRFBlockedError.
func isSSRFBlocked(err error) bool {
	var e *egressadapter.SSRFBlockedError
	return errors.As(err, &e)
}

// contains is a simple substring check used in error message assertions.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
