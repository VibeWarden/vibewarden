package ipfilter_test

import (
	"net"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/ipfilter"
)

// TestParseList_Valid verifies that valid IP and CIDR strings are accepted.
func TestParseList_Valid(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
	}{
		{"empty list", []string{}},
		{"single IPv4", []string{"192.168.1.1"}},
		{"IPv4 CIDR", []string{"10.0.0.0/8"}},
		{"single IPv6", []string{"2001:db8::1"}},
		{"IPv6 CIDR", []string{"2001:db8::/32"}},
		{"mixed", []string{"10.0.0.0/8", "192.168.1.100", "2001:db8::/32"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ipfilter.ParseList(tt.addresses)
			if err != nil {
				t.Errorf("ParseList(%v) unexpected error: %v", tt.addresses, err)
			}
		})
	}
}

// TestParseList_Invalid verifies that an unparseable entry returns an error.
func TestParseList_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{"hostname", "example.com"},
		{"garbage", "not-an-ip"},
		{"partial CIDR", "10.0.0/8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ipfilter.ParseList([]string{tt.address})
			if err == nil {
				t.Errorf("ParseList([%q]) expected error, got nil", tt.address)
			}
		})
	}
}

// TestList_MatchesAny verifies IP matching logic for both plain IPs and CIDRs.
func TestList_MatchesAny(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		clientIP  string
		want      bool
	}{
		{"exact IPv4 match", []string{"192.168.1.100"}, "192.168.1.100", true},
		{"exact IPv4 no match", []string{"192.168.1.100"}, "192.168.1.101", false},
		{"IPv4 CIDR match", []string{"10.0.0.0/8"}, "10.255.255.254", true},
		{"IPv4 CIDR no match", []string{"10.0.0.0/8"}, "11.0.0.1", false},
		{"empty list", []string{}, "10.0.0.1", false},
		{"IPv6 exact match", []string{"2001:db8::1"}, "2001:db8::1", true},
		{"IPv6 exact no match", []string{"2001:db8::1"}, "2001:db8::2", false},
		{"IPv6 CIDR match", []string{"2001:db8::/32"}, "2001:db8::cafe", true},
		{"IPv6 CIDR no match", []string{"2001:db8::/32"}, "2001:db9::1", false},
		{"nil ip", []string{"192.168.1.1"}, "", false}, // handled specially below
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := ipfilter.ParseList(tt.addresses)
			if err != nil {
				t.Fatalf("ParseList() error: %v", err)
			}

			var ip net.IP
			if tt.clientIP != "" {
				ip = net.ParseIP(tt.clientIP)
				if ip == nil {
					t.Fatalf("test setup: net.ParseIP(%q) returned nil", tt.clientIP)
				}
			}

			got := list.MatchesAny(ip)
			if got != tt.want {
				t.Errorf("MatchesAny(%q) = %v, want %v", tt.clientIP, got, tt.want)
			}
		})
	}
}

// TestList_MatchesAny_NilIP verifies that a nil net.IP never matches.
func TestList_MatchesAny_NilIP(t *testing.T) {
	list, err := ipfilter.ParseList([]string{"192.168.1.1", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseList() error: %v", err)
	}
	if list.MatchesAny(nil) {
		t.Error("MatchesAny(nil) = true, want false")
	}
}

// TestIsBlocked_Blocklist verifies IsBlocked in blocklist mode.
func TestIsBlocked_Blocklist(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		clientIP  string
		want      bool
	}{
		{"IP in list — blocked", []string{"10.0.0.1"}, "10.0.0.1", true},
		{"IP in CIDR — blocked", []string{"10.0.0.0/8"}, "10.1.2.3", true},
		{"IP not in list — allowed", []string{"10.0.0.1"}, "203.0.113.5", false},
		{"empty list — all allowed", []string{}, "10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := ipfilter.ParseList(tt.addresses)
			if err != nil {
				t.Fatalf("ParseList() error: %v", err)
			}
			ip := net.ParseIP(tt.clientIP)
			if ip == nil {
				t.Fatalf("test setup: net.ParseIP(%q) returned nil", tt.clientIP)
			}

			got := ipfilter.IsBlocked(ip, list, ipfilter.ModeBlocklist)
			if got != tt.want {
				t.Errorf("IsBlocked(%q, blocklist) = %v, want %v", tt.clientIP, got, tt.want)
			}
		})
	}
}

// TestIsBlocked_Allowlist verifies IsBlocked in allowlist mode.
func TestIsBlocked_Allowlist(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		clientIP  string
		want      bool
	}{
		{"IP in list — allowed", []string{"203.0.113.5"}, "203.0.113.5", false},
		{"IP in CIDR — allowed", []string{"203.0.113.0/24"}, "203.0.113.42", false},
		{"IP not in list — blocked", []string{"203.0.113.5"}, "192.168.1.100", true},
		{"empty list — all blocked", []string{}, "203.0.113.5", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := ipfilter.ParseList(tt.addresses)
			if err != nil {
				t.Fatalf("ParseList() error: %v", err)
			}
			ip := net.ParseIP(tt.clientIP)
			if ip == nil {
				t.Fatalf("test setup: net.ParseIP(%q) returned nil", tt.clientIP)
			}

			got := ipfilter.IsBlocked(ip, list, ipfilter.ModeAllowlist)
			if got != tt.want {
				t.Errorf("IsBlocked(%q, allowlist) = %v, want %v", tt.clientIP, got, tt.want)
			}
		})
	}
}

// TestIsBlocked_UnknownMode verifies that an unrecognised mode falls back to
// blocklist semantics (matching IP is blocked).
func TestIsBlocked_UnknownMode(t *testing.T) {
	list, err := ipfilter.ParseList([]string{"10.0.0.1"})
	if err != nil {
		t.Fatalf("ParseList() error: %v", err)
	}
	ip := net.ParseIP("10.0.0.1")

	// Unknown mode should behave like blocklist: IP in list => blocked.
	if !ipfilter.IsBlocked(ip, list, ipfilter.Mode("unknown")) {
		t.Error("IsBlocked with unknown mode and IP in list = false, want true (blocklist fallback)")
	}
}

// TestIsBlocked_NilIP verifies that a nil IP is never blocked in blocklist mode
// (nil never matches) and is always blocked in allowlist mode (nil never
// matches the allowlist).
func TestIsBlocked_NilIP(t *testing.T) {
	list, err := ipfilter.ParseList([]string{"10.0.0.1"})
	if err != nil {
		t.Fatalf("ParseList() error: %v", err)
	}

	if ipfilter.IsBlocked(nil, list, ipfilter.ModeBlocklist) {
		t.Error("IsBlocked(nil, blocklist) = true, want false (nil never matches)")
	}
	if !ipfilter.IsBlocked(nil, list, ipfilter.ModeAllowlist) {
		t.Error("IsBlocked(nil, allowlist) = false, want true (nil never in allowlist)")
	}
}
