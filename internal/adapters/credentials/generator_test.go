package credentials_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/credentials"
)

// urlSafeBase64RE matches URL-safe base64 characters (no padding).
var urlSafeBase64RE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGenerator_Generate_ReturnsUniqueValues(t *testing.T) {
	g := credentials.NewGenerator()
	ctx := context.Background()

	creds1, err := g.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate() first call unexpected error: %v", err)
	}

	creds2, err := g.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate() second call unexpected error: %v", err)
	}

	tests := []struct {
		name string
		v1   string
		v2   string
	}{
		{"PostgresPassword", creds1.PostgresPassword, creds2.PostgresPassword},
		{"KratosCookieSecret", creds1.KratosCookieSecret, creds2.KratosCookieSecret},
		{"KratosCipherSecret", creds1.KratosCipherSecret, creds2.KratosCipherSecret},
		{"GrafanaAdminPassword", creds1.GrafanaAdminPassword, creds2.GrafanaAdminPassword},
		{"OpenBaoDevRootToken", creds1.OpenBaoDevRootToken, creds2.OpenBaoDevRootToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.v1 == tt.v2 {
				t.Errorf("Generate() returned identical %s across two calls: %q", tt.name, tt.v1)
			}
		})
	}
}

func TestGenerator_Generate_CorrectLengths(t *testing.T) {
	g := credentials.NewGenerator()
	ctx := context.Background()

	creds, err := g.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	tests := []struct {
		name       string
		value      string
		wantLength int
	}{
		{"PostgresPassword", creds.PostgresPassword, 32},
		{"KratosCookieSecret", creds.KratosCookieSecret, 32},
		{"KratosCipherSecret", creds.KratosCipherSecret, 32},
		{"GrafanaAdminPassword", creds.GrafanaAdminPassword, 24},
		{"OpenBaoDevRootToken", creds.OpenBaoDevRootToken, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.value) != tt.wantLength {
				t.Errorf("Generate() %s has length %d, want %d", tt.name, len(tt.value), tt.wantLength)
			}
		})
	}
}

func TestGenerator_Generate_URLSafeCharacters(t *testing.T) {
	g := credentials.NewGenerator()
	ctx := context.Background()

	creds, err := g.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	tests := []struct {
		name  string
		value string
	}{
		{"PostgresPassword", creds.PostgresPassword},
		{"KratosCookieSecret", creds.KratosCookieSecret},
		{"KratosCipherSecret", creds.KratosCipherSecret},
		{"GrafanaAdminPassword", creds.GrafanaAdminPassword},
		{"OpenBaoDevRootToken", creds.OpenBaoDevRootToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !urlSafeBase64RE.MatchString(tt.value) {
				t.Errorf("Generate() %s contains non-URL-safe characters: %q", tt.name, tt.value)
			}
		})
	}
}
