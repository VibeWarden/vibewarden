package generate_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// TestGenerate_Integration_KratosOIDCRendering verifies that the real template
// renderer produces a kratos.yml that includes the OIDC method block when
// social providers are configured and omits it when they are not.
func TestGenerate_Integration_KratosOIDCRendering(t *testing.T) {
	renderer := template.NewRenderer(templates.FS)
	svc := generate.NewService(renderer)

	tests := []struct {
		name           string
		providers      []config.SocialProviderConfig
		wantSubstrings []string
		wantAbsent     []string
	}{
		{
			name:      "no social providers — no OIDC section",
			providers: nil,
			wantAbsent: []string{
				"oidc:",
				"providers:",
			},
		},
		{
			name: "google provider — OIDC section present with google mapper",
			providers: []config.SocialProviderConfig{
				{Provider: "google", ClientID: "my-google-client", ClientSecret: "my-google-secret"},
			},
			wantSubstrings: []string{
				"oidc:",
				"enabled: true",
				"providers:",
				"id: google",
				"provider: google",
				"client_id: my-google-client",
				"client_secret: my-google-secret",
				"mapper_url: file:///etc/kratos/mappers/google.jsonnet",
				"- email",
				"- profile",
			},
		},
		{
			name: "github provider — OIDC section present with github mapper",
			providers: []config.SocialProviderConfig{
				{Provider: "github", ClientID: "ghid", ClientSecret: "ghsecret"},
			},
			wantSubstrings: []string{
				"oidc:",
				"id: github",
				"provider: github",
				"mapper_url: file:///etc/kratos/mappers/github.jsonnet",
				"- user:email",
			},
		},
		{
			name: "generic OIDC provider — uses generic mapper and issuer_url",
			providers: []config.SocialProviderConfig{
				{
					Provider:     "oidc",
					ID:           "acme-sso",
					ClientID:     "oidc-cid",
					ClientSecret: "oidc-secret",
					IssuerURL:    "https://sso.acme.example",
				},
			},
			wantSubstrings: []string{
				"oidc:",
				"id: acme-sso",
				"provider: oidc",
				"mapper_url: file:///etc/kratos/mappers/generic.jsonnet",
				"issuer_url: https://sso.acme.example",
			},
		},
		{
			name: "explicit scopes override provider defaults",
			providers: []config.SocialProviderConfig{
				{
					Provider:     "google",
					ClientID:     "gcid",
					ClientSecret: "gsec",
					Scopes:       []string{"openid", "email"},
				},
			},
			wantSubstrings: []string{
				"- openid",
				"- email",
			},
			wantAbsent: []string{
				"- profile",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			cfg := minimalConfig()
			cfg.Auth.SocialProviders = tt.providers

			if err := svc.Generate(context.Background(), cfg, outputDir); err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			kratosYML := filepath.Join(outputDir, "kratos", "kratos.yml")
			data, err := os.ReadFile(kratosYML)
			if err != nil {
				t.Fatalf("reading kratos.yml: %v", err)
			}

			for _, want := range tt.wantSubstrings {
				if !bytes.Contains(data, []byte(want)) {
					t.Errorf("kratos.yml missing expected substring %q\n--- content ---\n%s", want, data)
				}
			}

			for _, absent := range tt.wantAbsent {
				if bytes.Contains(data, []byte(absent)) {
					t.Errorf("kratos.yml contains unexpected substring %q\n--- content ---\n%s", absent, data)
				}
			}
		})
	}
}
