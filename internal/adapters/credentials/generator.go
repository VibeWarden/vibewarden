// Package credentials provides adapters for credential generation and storage.
package credentials

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/domain/generate"
)

// Generator implements ports.CredentialGenerator using crypto/rand.
type Generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() *Generator {
	return &Generator{}
}

// Generate creates cryptographically secure random credentials.
func (g *Generator) Generate(_ context.Context) (*generate.GeneratedCredentials, error) {
	postgres, err := randomAlphanumeric(32)
	if err != nil {
		return nil, fmt.Errorf("generating postgres password: %w", err)
	}

	cookie, err := randomAlphanumeric(32)
	if err != nil {
		return nil, fmt.Errorf("generating kratos cookie secret: %w", err)
	}

	cipher, err := randomAlphanumeric(32)
	if err != nil {
		return nil, fmt.Errorf("generating kratos cipher secret: %w", err)
	}

	grafana, err := randomAlphanumeric(24)
	if err != nil {
		return nil, fmt.Errorf("generating grafana admin password: %w", err)
	}

	bao, err := randomAlphanumeric(32)
	if err != nil {
		return nil, fmt.Errorf("generating openbao root token: %w", err)
	}

	return &generate.GeneratedCredentials{
		PostgresPassword:     postgres,
		KratosCookieSecret:   cookie,
		KratosCipherSecret:   cipher,
		GrafanaAdminPassword: grafana,
		OpenBaoDevRootToken:  bao,
	}, nil
}

// randomAlphanumeric generates a random URL-safe base64 string of the specified length.
func randomAlphanumeric(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Use URL-safe base64 without padding for alphanumeric-ish output.
	encoded := base64.RawURLEncoding.EncodeToString(bytes)
	if len(encoded) < length {
		return "", fmt.Errorf("encoded string too short: got %d, want %d", len(encoded), length)
	}
	return encoded[:length], nil
}
