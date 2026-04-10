package secret_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/secret"
)

func TestRetrievedSecret_FieldsAccessible(t *testing.T) {
	s := secret.RetrievedSecret{
		Path:   "infra/postgres",
		Alias:  "postgres",
		Data:   map[string]string{"username": "admin", "password": "secret"},
		Source: secret.SourceOpenBao,
	}

	if s.Path != "infra/postgres" {
		t.Errorf("Path = %q, want %q", s.Path, "infra/postgres")
	}
	if s.Alias != "postgres" {
		t.Errorf("Alias = %q, want %q", s.Alias, "postgres")
	}
	if s.Data["username"] != "admin" {
		t.Errorf("Data[username] = %q, want %q", s.Data["username"], "admin")
	}
	if s.Source != secret.SourceOpenBao {
		t.Errorf("Source = %q, want %q", s.Source, secret.SourceOpenBao)
	}
}

func TestSecretSource_Constants(t *testing.T) {
	if secret.SourceOpenBao != "openbao" {
		t.Errorf("SourceOpenBao = %q, want %q", secret.SourceOpenBao, "openbao")
	}
	if secret.SourceCredentialsFile != "credentials_file" {
		t.Errorf("SourceCredentialsFile = %q, want %q", secret.SourceCredentialsFile, "credentials_file")
	}
}
