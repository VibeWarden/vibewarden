package caddy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestBuildACMEIssuers(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ports.TLSConfig
		wantCount   int
		wantFirstCA string
		wantEmail   bool
	}{
		{
			name: "letsencrypt without acme_ca produces 3-issuer fallback chain",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
			},
			wantCount:   3,
			wantFirstCA: acmeURLLetsEncrypt,
		},
		{
			name: "letsencrypt with acme_ca produces single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				ACMECA:   "https://custom-ca.example.com/directory",
			},
			wantCount:   1,
			wantFirstCA: "https://custom-ca.example.com/directory",
		},
		{
			name: "zerossl produces single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderZeroSSL,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCount:   1,
			wantFirstCA: acmeURLZeroSSL,
			wantEmail:   true,
		},
		{
			name: "buypass produces single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderBuypass,
				Domain:   "example.com",
			},
			wantCount:   1,
			wantFirstCA: acmeURLBuypass,
		},
		{
			name: "letsencrypt-staging produces single issuer",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncryptStaging,
				Domain:   "example.com",
			},
			wantCount:   1,
			wantFirstCA: acmeURLLetsEncryptStaging,
		},
		{
			name: "letsencrypt with email includes email in all issuers",
			cfg: ports.TLSConfig{
				Provider: ports.TLSProviderLetsEncrypt,
				Domain:   "example.com",
				Email:    "admin@example.com",
			},
			wantCount:   3,
			wantFirstCA: acmeURLLetsEncrypt,
			wantEmail:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuers := buildACMEIssuers(tt.cfg)

			if len(issuers) != tt.wantCount {
				t.Fatalf("got %d issuers, want %d", len(issuers), tt.wantCount)
			}

			// Verify all issuers have module "acme".
			for i, issuer := range issuers {
				if issuer["module"] != "acme" {
					t.Errorf("issuer[%d].module = %q, want %q", i, issuer["module"], "acme")
				}
			}

			// Verify first issuer has expected CA.
			if issuers[0]["ca"] != tt.wantFirstCA {
				t.Errorf("first issuer ca = %q, want %q", issuers[0]["ca"], tt.wantFirstCA)
			}

			// Verify email presence.
			for i, issuer := range issuers {
				_, hasEmail := issuer["email"]
				if tt.wantEmail && !hasEmail {
					t.Errorf("issuer[%d] missing email field", i)
				}
				if !tt.wantEmail && hasEmail {
					t.Errorf("issuer[%d] has unexpected email field", i)
				}
			}
		})
	}
}

func TestBuildACMEIssuers_FallbackChainOrder(t *testing.T) {
	cfg := ports.TLSConfig{
		Provider: ports.TLSProviderLetsEncrypt,
		Domain:   "example.com",
	}

	issuers := buildACMEIssuers(cfg)

	if len(issuers) != 3 {
		t.Fatalf("expected 3 issuers, got %d", len(issuers))
	}

	wantOrder := []string{
		acmeURLLetsEncrypt,
		acmeURLZeroSSL,
		acmeURLBuypass,
	}

	for i, want := range wantOrder {
		got := issuers[i]["ca"]
		if got != want {
			t.Errorf("issuer[%d].ca = %q, want %q", i, got, want)
		}
	}
}

func TestBuildSingleACMEIssuer(t *testing.T) {
	tests := []struct {
		name      string
		caURL     string
		email     string
		wantCA    string
		wantEmail string
	}{
		{
			name:   "with email",
			caURL:  acmeURLLetsEncrypt,
			email:  "admin@example.com",
			wantCA: acmeURLLetsEncrypt,
		},
		{
			name:   "without email",
			caURL:  acmeURLBuypass,
			email:  "",
			wantCA: acmeURLBuypass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := buildSingleACMEIssuer(tt.caURL, tt.email)

			if issuer["module"] != "acme" {
				t.Errorf("module = %q, want %q", issuer["module"], "acme")
			}
			if issuer["ca"] != tt.wantCA {
				t.Errorf("ca = %q, want %q", issuer["ca"], tt.wantCA)
			}

			emailVal, hasEmail := issuer["email"]
			if tt.email != "" {
				if !hasEmail {
					t.Error("expected email field in issuer")
				} else if emailVal != tt.email {
					t.Errorf("email = %q, want %q", emailVal, tt.email)
				}
			} else {
				if hasEmail {
					t.Errorf("unexpected email field in issuer: %v", emailVal)
				}
			}
		})
	}
}

func TestIsACMEProvider(t *testing.T) {
	tests := []struct {
		provider ports.TLSProvider
		want     bool
	}{
		{ports.TLSProviderLetsEncrypt, true},
		{ports.TLSProviderZeroSSL, true},
		{ports.TLSProviderBuypass, true},
		{ports.TLSProviderLetsEncryptStaging, true},
		{ports.TLSProviderSelfSigned, false},
		{ports.TLSProviderExternal, false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			got := isACMEProvider(tt.provider)
			if got != tt.want {
				t.Errorf("isACMEProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}
