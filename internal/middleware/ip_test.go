package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name        string
		remoteAddr  string
		xForwardFor string
		trustProxy  bool
		want        string
	}{
		// Direct connection — RemoteAddr used regardless of trustProxy.
		{
			name:       "ipv4 from RemoteAddr",
			remoteAddr: "192.168.1.42:54321",
			trustProxy: false,
			want:       "192.168.1.42",
		},
		{
			name:       "ipv6 from RemoteAddr",
			remoteAddr: "[::1]:12345",
			trustProxy: false,
			want:       "::1",
		},
		{
			name:       "RemoteAddr bare ip no port",
			remoteAddr: "10.0.0.1",
			trustProxy: false,
			want:       "10.0.0.1",
		},
		// X-Forwarded-For ignored when trustProxy is false.
		{
			name:        "XFF ignored when trustProxy false",
			remoteAddr:  "10.0.0.2:9000",
			xForwardFor: "1.2.3.4",
			trustProxy:  false,
			want:        "10.0.0.2",
		},
		// X-Forwarded-For used when trustProxy is true.
		{
			name:        "XFF single ip trusted",
			remoteAddr:  "10.0.0.3:9000",
			xForwardFor: "203.0.113.5",
			trustProxy:  true,
			want:        "203.0.113.5",
		},
		{
			name:        "XFF multiple ips uses leftmost",
			remoteAddr:  "10.0.0.4:9000",
			xForwardFor: "198.51.100.7, 10.0.0.1, 172.16.0.1",
			trustProxy:  true,
			want:        "198.51.100.7",
		},
		{
			name:        "XFF with spaces around ip",
			remoteAddr:  "10.0.0.5:9000",
			xForwardFor: "  198.51.100.8  ",
			trustProxy:  true,
			want:        "198.51.100.8",
		},
		// Invalid XFF falls back to RemoteAddr.
		{
			name:        "XFF invalid falls back to RemoteAddr",
			remoteAddr:  "192.168.0.1:8080",
			xForwardFor: "not-an-ip",
			trustProxy:  true,
			want:        "192.168.0.1",
		},
		// IPv6 in X-Forwarded-For.
		{
			name:        "XFF ipv6 trusted",
			remoteAddr:  "10.0.0.6:9000",
			xForwardFor: "2001:db8::1",
			trustProxy:  true,
			want:        "2001:db8::1",
		},
		// X-Forwarded-For absent but trustProxy true — falls back to RemoteAddr.
		{
			name:       "trustProxy true but no XFF uses RemoteAddr",
			remoteAddr: "172.16.0.9:4321",
			trustProxy: true,
			want:       "172.16.0.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xForwardFor != "" {
				r.Header.Set("X-Forwarded-For", tt.xForwardFor)
			}

			got := ExtractClientIP(r, tt.trustProxy)
			if got != tt.want {
				t.Errorf("ExtractClientIP(trustProxy=%v) = %q, want %q", tt.trustProxy, got, tt.want)
			}
		})
	}
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"valid ipv4", "192.168.1.1", "192.168.1.1"},
		{"valid ipv6 loopback", "::1", "::1"},
		{"valid ipv6 full", "2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"valid ipv6 canonical", "2001:db8::1", "2001:db8::1"},
		{"empty string", "", ""},
		{"not an ip", "not-an-ip", ""},
		{"ip with port", "1.2.3.4:80", ""},
		{"leading spaces", "  10.0.0.1", "10.0.0.1"},
		{"trailing spaces", "10.0.0.1  ", "10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeIP(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
