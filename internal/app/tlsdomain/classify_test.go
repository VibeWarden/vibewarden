package tlsdomain_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/tlsdomain"
)

func TestIsACMEIncompatible(t *testing.T) {
	tests := []struct {
		name           string
		domain         string
		wantIncompat   bool
		wantReasonPart string // substring of reason; empty means any is fine
	}{
		// Compatible domains (ACME can issue).
		{"empty string is compatible", "", false, ""},
		{"public domain is compatible", "example.com", false, ""},
		{"subdomain is compatible", "sub.example.com", false, ""},
		{"deep subdomain is compatible", "a.b.example.com", false, ""},
		{"trailing dot normalised", "example.com.", false, ""},
		{"uppercase normalised", "EXAMPLE.COM", false, ""},

		// localhost
		{"bare localhost is incompatible", "localhost", true, "localhost"},
		{"uppercase LOCALHOST normalised", "LOCALHOST", true, "localhost"},
		{"localhost with trailing dot", "localhost.", true, "localhost"},

		// IP literals
		{"IPv4 loopback is incompatible", "127.0.0.1", true, "IP literal"},
		{"IPv4 RFC1918 10.x is incompatible", "10.0.0.1", true, "IP literal"},
		{"IPv4 RFC1918 192.168.x is incompatible", "192.168.1.1", true, "IP literal"},
		{"IPv6 loopback is incompatible", "::1", true, "IP literal"},
		{"IPv4-mapped IPv6 is incompatible", "::ffff:192.168.0.1", true, "IP literal"},

		// Reserved TLDs
		{".local TLD is incompatible", "myapp.local", true, "reserved TLD .local"},
		{".localhost TLD is incompatible", "myapp.localhost", true, "reserved TLD .localhost"},
		{".test TLD is incompatible", "myapp.test", true, "reserved TLD .test"},
		{".invalid TLD is incompatible", "myapp.invalid", true, "reserved TLD .invalid"},
		{".example TLD is incompatible", "myapp.example", true, "reserved TLD .example"},
		{"subdomain under .local is incompatible", "a.b.local", true, "reserved TLD .local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			incompat, reason := tlsdomain.IsACMEIncompatible(tt.domain)

			if incompat != tt.wantIncompat {
				t.Errorf("IsACMEIncompatible(%q) incompat = %v, want %v (reason=%q)",
					tt.domain, incompat, tt.wantIncompat, reason)
			}

			if tt.wantIncompat && tt.wantReasonPart != "" {
				if reason != tt.wantReasonPart {
					t.Errorf("IsACMEIncompatible(%q) reason = %q, want %q",
						tt.domain, reason, tt.wantReasonPart)
				}
			}

			if !tt.wantIncompat && reason != "" {
				t.Errorf("IsACMEIncompatible(%q) compatible domain returned non-empty reason %q",
					tt.domain, reason)
			}
		})
	}
}
