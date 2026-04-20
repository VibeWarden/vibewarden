package config_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeSecretReader is a simple in-memory SecretKVReader for testing.
type fakeSecretReader struct {
	data map[string]map[string]string
}

func (f *fakeSecretReader) Get(_ context.Context, path string) (map[string]string, error) {
	d, ok := f.data[path]
	if !ok {
		return nil, ports.ErrSecretNotFound
	}
	return d, nil
}

func TestResolveSecrets_ValidURI(t *testing.T) {
	store := &fakeSecretReader{
		data: map[string]map[string]string{
			"auth/google": {
				"client_id":     "google-id-123",
				"client_secret": "google-secret-456",
			},
		},
	}

	cfg := &config.Config{}
	// Use a known string field that would plausibly hold a secret URI.
	// Admin.Token is a simple top-level string field.
	cfg.Admin.Token = "secret://auth/google/client_id"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v", err)
	}

	if cfg.Admin.Token != "google-id-123" {
		t.Errorf("Admin.Token = %q, want %q", cfg.Admin.Token, "google-id-123")
	}
}

func TestResolveSecrets_MultipleFields(t *testing.T) {
	store := &fakeSecretReader{
		data: map[string]map[string]string{
			"auth/google": {
				"client_id":     "google-id-123",
				"client_secret": "google-secret-456",
			},
			"infra/db": {
				"password": "db-pass-789",
			},
		},
	}

	cfg := &config.Config{}
	cfg.Admin.Token = "secret://auth/google/client_id"
	cfg.Database.URL = "secret://infra/db/password"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v", err)
	}

	if cfg.Admin.Token != "google-id-123" {
		t.Errorf("Admin.Token = %q, want %q", cfg.Admin.Token, "google-id-123")
	}
	if cfg.Database.URL != "db-pass-789" {
		t.Errorf("Database.URL = %q, want %q", cfg.Database.URL, "db-pass-789")
	}
}

func TestResolveSecrets_NonSecretFieldsUntouched(t *testing.T) {
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}
	cfg.Profile = "prod"
	cfg.Server.Host = "127.0.0.1"
	cfg.Admin.Token = "plain-token-value"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v", err)
	}

	if cfg.Profile != "prod" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "prod")
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
	}
	if cfg.Admin.Token != "plain-token-value" {
		t.Errorf("Admin.Token = %q, want %q", cfg.Admin.Token, "plain-token-value")
	}
}

func TestResolveSecrets_MissingPath(t *testing.T) {
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}
	cfg.Admin.Token = "secret://nonexistent/path/key"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("ResolveSecrets() expected error for missing path, got nil")
	}

	if !strings.Contains(err.Error(), "nonexistent/path") {
		t.Errorf("error should mention the path, got: %v", err)
	}
	if !errors.Is(err, ports.ErrSecretNotFound) {
		t.Errorf("error should wrap ErrSecretNotFound, got: %v", err)
	}
}

func TestResolveSecrets_MissingKey(t *testing.T) {
	store := &fakeSecretReader{
		data: map[string]map[string]string{
			"auth/google": {
				"client_id": "google-id-123",
			},
		},
	}

	cfg := &config.Config{}
	cfg.Admin.Token = "secret://auth/google/nonexistent_key"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("ResolveSecrets() expected error for missing key, got nil")
	}

	if !strings.Contains(err.Error(), "nonexistent_key") {
		t.Errorf("error should mention the missing key, got: %v", err)
	}
	if !strings.Contains(err.Error(), "auth/google") {
		t.Errorf("error should mention the path, got: %v", err)
	}
}

func TestResolveSecrets_InvalidURIFormat(t *testing.T) {
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}
	// secret:// with no path/key separator
	cfg.Admin.Token = "secret://nokeyseparator"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("ResolveSecrets() expected error for invalid URI format, got nil")
	}

	if !strings.Contains(err.Error(), "nokeyseparator") {
		t.Errorf("error should mention the invalid URI, got: %v", err)
	}
}

func TestResolveSecrets_SecretsConfigSkipped(t *testing.T) {
	// The Secrets config section itself should never be resolved, even if it
	// contains a secret:// URI -- this prevents circular bootstrap.
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}
	cfg.Secrets.Builtin.Path = "secret://should/not/resolve"
	cfg.Secrets.Builtin.KeyFile = "secret://should/not/resolve"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v; Secrets section should be skipped", err)
	}

	// Values should remain unchanged.
	if cfg.Secrets.Builtin.Path != "secret://should/not/resolve" {
		t.Errorf("Secrets.Builtin.Path was resolved; should have been skipped")
	}
	if cfg.Secrets.Builtin.KeyFile != "secret://should/not/resolve" {
		t.Errorf("Secrets.Builtin.KeyFile was resolved; should have been skipped")
	}
}

func TestResolveSecrets_FieldPathInError(t *testing.T) {
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}
	cfg.Admin.Token = "secret://missing/path/key"

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error message should include the struct field path.
	if !strings.Contains(err.Error(), "Admin") && !strings.Contains(err.Error(), "Token") {
		t.Errorf("error should include field path like Admin.Token, got: %v", err)
	}
}

func TestResolveSecrets_EmptyConfig(t *testing.T) {
	store := &fakeSecretReader{data: map[string]map[string]string{}}

	cfg := &config.Config{}

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v for empty config", err)
	}
}

func TestResolveSecrets_StringSliceField(t *testing.T) {
	store := &fakeSecretReader{
		data: map[string]map[string]string{
			"auth/api": {
				"key1": "resolved-key-1",
			},
		},
	}

	cfg := &config.Config{}
	cfg.Auth.PublicPaths = []string{"/health", "secret://auth/api/key1", "/ready"}

	err := config.ResolveSecrets(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("ResolveSecrets() error = %v", err)
	}

	if cfg.Auth.PublicPaths[0] != "/health" {
		t.Errorf("PublicPaths[0] = %q, want %q", cfg.Auth.PublicPaths[0], "/health")
	}
	if cfg.Auth.PublicPaths[1] != "resolved-key-1" {
		t.Errorf("PublicPaths[1] = %q, want %q", cfg.Auth.PublicPaths[1], "resolved-key-1")
	}
	if cfg.Auth.PublicPaths[2] != "/ready" {
		t.Errorf("PublicPaths[2] = %q, want %q", cfg.Auth.PublicPaths[2], "/ready")
	}
}
