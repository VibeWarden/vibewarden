package caddy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestIsACMEProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider ports.TLSProvider
		want     bool
	}{
		{"letsencrypt", ports.TLSProviderLetsEncrypt, true},
		{"zerossl", ports.TLSProviderZeroSSL, true},
		{"buypass", ports.TLSProviderBuypass, true},
		{"letsencrypt-staging", ports.TLSProviderLetsEncryptStaging, true},
		{"self-signed", ports.TLSProviderSelfSigned, false},
		{"external", ports.TLSProviderExternal, false},
		{"empty string", "", false},
		{"unknown", ports.TLSProvider("cloudflare"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isACMEProvider(tt.provider)
			if got != tt.want {
				t.Errorf("isACMEProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestBuildACMEIssuers(t *testing.T) {
	tests := []struct {
		name       string
		cfg        ports.TLSConfig
		wantCount  int
		wantCAs    []string
		wantEmails []string // expected email for each issuer; empty means no email field
	}{
		{
			name: "letsencrypt default — 3-issuer fallback chain",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
			wantCount: 3,
			wantCAs: []string{
				acmeCALetsEncrypt,
				acmeCAZeroSSL,
				acmeCABuypass,
			},
			wantEmails: []string{"", "", ""},
		},
		{
			name: "letsencrypt default with email — email propagated to all issuers",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCount: 3,
			wantCAs: []string{
				acmeCALetsEncrypt,
				acmeCAZeroSSL,
				acmeCABuypass,
			},
			wantEmails: []string{"admin@example.com", "admin@example.com", "admin@example.com"},
		},
		{
			name: "letsencrypt with acme_ca override — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				ACMECA:   "https://custom-ca.example.com/directory",
			},
			wantCount:  1,
			wantCAs:    []string{"https://custom-ca.example.com/directory"},
			wantEmails: []string{""},
		},
		{
			name: "letsencrypt with acme_ca and email — single issuer with email",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				ACMECA:   "https://custom-ca.example.com/directory",
				Email:    "admin@example.com",
			},
			wantCount:  1,
			wantCAs:    []string{"https://custom-ca.example.com/directory"},
			wantEmails: []string{"admin@example.com"},
		},
		{
			name: "zerossl — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderZeroSSL,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCount:  1,
			wantCAs:    []string{acmeCAZeroSSL},
			wantEmails: []string{"admin@example.com"},
		},
		{
			name: "buypass — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderBuypass,
				Domain:   "example.com",
			},
			wantCount:  1,
			wantCAs:    []string{acmeCABuypass},
			wantEmails: []string{""},
		},
		{
			name: "letsencrypt-staging — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncryptStaging,
				Domain:   "example.com",
			},
			wantCount:  1,
			wantCAs:    []string{acmeCALetsEncryptStaging},
			wantEmails: []string{""},
		},
		{
			name: "letsencrypt-staging with email",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncryptStaging,
				Domain:   "example.com",
				Email:    "dev@example.com",
			},
			wantCount:  1,
			wantCAs:    []string{acmeCALetsEncryptStaging},
			wantEmails: []string{"dev@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuers := buildACMEIssuers(tt.cfg)

			if len(issuers) != tt.wantCount {
				t.Fatalf("buildACMEIssuers() returned %d issuers, want %d", len(issuers), tt.wantCount)
			}

			for i, issuer := range issuers {
				// Check module.
				if issuer["module"] != "acme" {
					t.Errorf("issuer[%d].module = %q, want %q", i, issuer["module"], "acme")
				}

				// Check CA URL.
				ca, ok := issuer["ca"].(string)
				if !ok || ca != tt.wantCAs[i] {
					t.Errorf("issuer[%d].ca = %q, want %q", i, ca, tt.wantCAs[i])
				}

				// Check email.
				emailVal, hasEmail := issuer["email"]
				if tt.wantEmails[i] != "" {
					if !hasEmail {
						t.Errorf("issuer[%d] missing email, want %q", i, tt.wantEmails[i])
					} else if emailVal != tt.wantEmails[i] {
						t.Errorf("issuer[%d].email = %q, want %q", i, emailVal, tt.wantEmails[i])
					}
				} else {
					if hasEmail {
						t.Errorf("issuer[%d] has unexpected email = %q", i, emailVal)
					}
				}
			}
		})
	}
}

func TestBuildSingleACMEIssuer(t *testing.T) {
	tests := []struct {
		name      string
		ca        string
		email     string
		wantEmail bool
	}{
		{
			name:      "with email",
			ca:        acmeCALetsEncrypt,
			email:     "admin@example.com",
			wantEmail: true,
		},
		{
			name:      "without email",
			ca:        acmeCAZeroSSL,
			email:     "",
			wantEmail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := buildSingleACMEIssuer(tt.ca, tt.email)

			if issuer["module"] != "acme" {
				t.Errorf("module = %q, want %q", issuer["module"], "acme")
			}
			if issuer["ca"] != tt.ca {
				t.Errorf("ca = %q, want %q", issuer["ca"], tt.ca)
			}
			_, hasEmail := issuer["email"]
			if tt.wantEmail && !hasEmail {
				t.Error("expected email field, got none")
			}
			if !tt.wantEmail && hasEmail {
				t.Errorf("unexpected email field: %v", issuer["email"])
			}
		})
	}
}
