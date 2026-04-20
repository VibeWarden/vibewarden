package tls

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestBuildACMEIssuers_Mirror mirrors the table-driven test in
// internal/adapters/caddy/acme_issuers_test.go to guarantee the duplicated
// copies stay in lockstep per ADR-083 §4.
func TestBuildACMEIssuers_Mirror(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ports.TLSConfig
		wantCAs     []string
		wantEmails  []string
		wantSkipped []SkippedIssuer
	}{
		{
			name: "letsencrypt default — no email, zerossl skipped",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
			wantCAs:    []string{acmeCALetsEncrypt},
			wantEmails: []string{""},
			wantSkipped: []SkippedIssuer{
				{Provider: string(ports.TLSProviderZeroSSL), Reason: skipReasonEmailNotConfigured},
			},
		},
		{
			name: "letsencrypt default with email — LE + ZeroSSL (no Buypass)",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCAs: []string{
				acmeCALetsEncrypt,
				acmeCAZeroSSL,
			},
			wantEmails:  []string{"admin@example.com", "admin@example.com"},
			wantSkipped: nil,
		},
		{
			name: "letsencrypt with acme_ca override — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				ACMECA:   "https://custom-ca.example.com/directory",
			},
			wantCAs:     []string{"https://custom-ca.example.com/directory"},
			wantEmails:  []string{""},
			wantSkipped: nil,
		},
		{
			name: "letsencrypt with acme_ca and email — single issuer with email",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				ACMECA:   "https://custom-ca.example.com/directory",
				Email:    "admin@example.com",
			},
			wantCAs:     []string{"https://custom-ca.example.com/directory"},
			wantEmails:  []string{"admin@example.com"},
			wantSkipped: nil,
		},
		{
			name: "zerossl — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderZeroSSL,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCAs:     []string{acmeCAZeroSSL},
			wantEmails:  []string{"admin@example.com"},
			wantSkipped: nil,
		},
		{
			name: "buypass — single issuer (explicit opt-in)",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderBuypass,
				Domain:   "example.com",
			},
			wantCAs:     []string{acmeCABuypass},
			wantEmails:  []string{""},
			wantSkipped: nil,
		},
		{
			name: "letsencrypt-staging — single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncryptStaging,
				Domain:   "example.com",
			},
			wantCAs:     []string{acmeCALetsEncryptStaging},
			wantEmails:  []string{""},
			wantSkipped: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuers, skipped := buildACMEIssuers(tt.cfg)

			if len(issuers) != len(tt.wantCAs) {
				t.Fatalf("buildACMEIssuers() returned %d issuers, want %d", len(issuers), len(tt.wantCAs))
			}
			for i, issuer := range issuers {
				if issuer["module"] != "acme" {
					t.Errorf("issuer[%d].module = %q, want %q", i, issuer["module"], "acme")
				}
				if ca, _ := issuer["ca"].(string); ca != tt.wantCAs[i] {
					t.Errorf("issuer[%d].ca = %q, want %q", i, ca, tt.wantCAs[i])
				}
				emailVal, hasEmail := issuer["email"]
				if tt.wantEmails[i] != "" {
					if !hasEmail {
						t.Errorf("issuer[%d] missing email, want %q", i, tt.wantEmails[i])
					} else if emailVal != tt.wantEmails[i] {
						t.Errorf("issuer[%d].email = %q, want %q", i, emailVal, tt.wantEmails[i])
					}
				} else if hasEmail {
					t.Errorf("issuer[%d] has unexpected email = %q", i, emailVal)
				}
			}

			if len(skipped) != len(tt.wantSkipped) {
				t.Fatalf("skipped len = %d, want %d: got %+v", len(skipped), len(tt.wantSkipped), skipped)
			}
			for i, sk := range skipped {
				if sk.Provider != tt.wantSkipped[i].Provider {
					t.Errorf("skipped[%d].Provider = %q, want %q", i, sk.Provider, tt.wantSkipped[i].Provider)
				}
				if sk.Reason != tt.wantSkipped[i].Reason {
					t.Errorf("skipped[%d].Reason = %q, want %q", i, sk.Reason, tt.wantSkipped[i].Reason)
				}
			}
		})
	}
}

// TestBuildACMEIssuers_NoBuypassInDefaultChain_Mirror is a regression guard
// for ADR-083: Buypass must never appear in the default fallback chain.
func TestBuildACMEIssuers_NoBuypassInDefaultChain_Mirror(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.TLSConfig
	}{
		{
			name: "default letsencrypt, no email",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
		},
		{
			name: "default letsencrypt, with email",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuers, _ := buildACMEIssuers(tt.cfg)
			for i, iss := range issuers {
				if iss["ca"] == acmeCABuypass {
					t.Errorf("issuer[%d].ca = buypass URL; Buypass must not appear in default chain per ADR-083", i)
				}
			}
		})
	}
}
