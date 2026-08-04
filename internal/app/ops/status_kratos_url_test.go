package ops

import "testing"

func TestHostKratosAdminURL(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		wantBase      string
		wantRewritten string
	}{
		{
			name:          "compose service name is rewritten to loopback",
			raw:           "http://kratos:4434",
			wantBase:      "http://127.0.0.1:4434",
			wantRewritten: "kratos:4434",
		},
		{
			name:          "compose service name without port",
			raw:           "http://kratos",
			wantBase:      "http://127.0.0.1",
			wantRewritten: "kratos",
		},
		{
			name:          "hyphenated compose service name",
			raw:           "https://kratos-admin:4434",
			wantBase:      "https://127.0.0.1:4434",
			wantRewritten: "kratos-admin:4434",
		},
		{
			name:          "trailing slash is trimmed",
			raw:           "http://kratos:4434/",
			wantBase:      "http://127.0.0.1:4434",
			wantRewritten: "kratos:4434",
		},
		{
			name:          "sub path is preserved",
			raw:           "http://kratos:4434/api",
			wantBase:      "http://127.0.0.1:4434/api",
			wantRewritten: "kratos:4434",
		},
		{
			name:     "loopback IP is unchanged",
			raw:      "http://127.0.0.1:4434",
			wantBase: "http://127.0.0.1:4434",
		},
		{
			name:     "localhost is unchanged",
			raw:      "http://localhost:4434",
			wantBase: "http://localhost:4434",
		},
		{
			name:     "IPv6 literal is unchanged",
			raw:      "http://[::1]:4434",
			wantBase: "http://[::1]:4434",
		},
		{
			name:     "external FQDN is unchanged",
			raw:      "https://kratos.example.com",
			wantBase: "https://kratos.example.com",
		},
		{
			name:     "empty URL is unchanged",
			raw:      "",
			wantBase: "",
		},
		{
			name:     "URL without scheme is unchanged",
			raw:      "kratos:4434",
			wantBase: "kratos:4434",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotRewritten := hostKratosAdminURL(tt.raw)
			if gotBase != tt.wantBase {
				t.Errorf("hostKratosAdminURL(%q) base = %q, want %q", tt.raw, gotBase, tt.wantBase)
			}
			if gotRewritten != tt.wantRewritten {
				t.Errorf("hostKratosAdminURL(%q) rewrittenFrom = %q, want %q", tt.raw, gotRewritten, tt.wantRewritten)
			}
		})
	}
}

func TestIsContainerInternalHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"compose service name", "kratos", true},
		{"hyphenated service name", "kratos-admin", true},
		{"uppercase localhost", "LocalHost", false},
		{"localhost", "localhost", false},
		{"IPv4 literal", "127.0.0.1", false},
		{"IPv6 literal", "::1", false},
		{"FQDN", "kratos.internal.example.com", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isContainerInternalHost(tt.host); got != tt.want {
				t.Errorf("isContainerInternalHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
