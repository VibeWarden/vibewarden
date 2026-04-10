package secret_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/secret"
)

func TestResolveAlias_WellKnown(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantOpenBaoPath string
		wantDynamicRole string
		wantCredKeys    []string
		wantEnvPrefix   string
	}{
		{
			name:            "postgres",
			input:           "postgres",
			wantOpenBaoPath: "infra/postgres",
			wantDynamicRole: "app-readwrite",
			wantCredKeys:    []string{"POSTGRES_PASSWORD"},
			wantEnvPrefix:   "POSTGRES_",
		},
		{
			name:            "kratos",
			input:           "kratos",
			wantOpenBaoPath: "infra/kratos",
			wantDynamicRole: "",
			wantCredKeys:    []string{"KRATOS_SECRETS_COOKIE", "KRATOS_SECRETS_CIPHER"},
			wantEnvPrefix:   "KRATOS_",
		},
		{
			name:            "grafana",
			input:           "grafana",
			wantOpenBaoPath: "infra/grafana",
			wantDynamicRole: "",
			wantCredKeys:    []string{"GRAFANA_ADMIN_PASSWORD"},
			wantEnvPrefix:   "GRAFANA_",
		},
		{
			name:            "openbao",
			input:           "openbao",
			wantOpenBaoPath: "",
			wantDynamicRole: "",
			wantCredKeys:    []string{"OPENBAO_DEV_ROOT_TOKEN"},
			wantEnvPrefix:   "OPENBAO_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.ResolveAlias(tt.input)
			if got == nil {
				t.Fatalf("ResolveAlias(%q) = nil, want non-nil", tt.input)
			}
			if got.Name != tt.input {
				t.Errorf("Name = %q, want %q", got.Name, tt.input)
			}
			if got.OpenBaoPath != tt.wantOpenBaoPath {
				t.Errorf("OpenBaoPath = %q, want %q", got.OpenBaoPath, tt.wantOpenBaoPath)
			}
			if got.DynamicRole != tt.wantDynamicRole {
				t.Errorf("DynamicRole = %q, want %q", got.DynamicRole, tt.wantDynamicRole)
			}
			for _, key := range tt.wantCredKeys {
				if _, ok := got.CredentialsFileKeys[key]; !ok {
					t.Errorf("CredentialsFileKeys missing key %q", key)
				}
			}
			if len(got.CredentialsFileKeys) != len(tt.wantCredKeys) {
				t.Errorf("CredentialsFileKeys len = %d, want %d", len(got.CredentialsFileKeys), len(tt.wantCredKeys))
			}
			if got.EnvPrefix != tt.wantEnvPrefix {
				t.Errorf("EnvPrefix = %q, want %q", got.EnvPrefix, tt.wantEnvPrefix)
			}
		})
	}
}

func TestResolveAlias_Unknown(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"unknown alias", "redis"},
		{"arbitrary path", "demo/api-key"},
		{"case sensitive upper", "Postgres"},
		{"case sensitive mixed", "POSTGRES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.ResolveAlias(tt.input)
			if got != nil {
				t.Errorf("ResolveAlias(%q) = %+v, want nil", tt.input, got)
			}
		})
	}
}

func TestListAliases(t *testing.T) {
	aliases := secret.ListAliases()

	// Must return all 4 well-known aliases.
	if len(aliases) != 4 {
		t.Fatalf("ListAliases() returned %d aliases, want 4", len(aliases))
	}

	names := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		names[a.Name] = true
	}

	for _, want := range []string{"postgres", "kratos", "grafana", "openbao"} {
		if !names[want] {
			t.Errorf("ListAliases() missing alias %q", want)
		}
	}
}

func TestListAliases_ReturnsCopy(t *testing.T) {
	aliases1 := secret.ListAliases()
	aliases1[0].Name = "mutated"

	aliases2 := secret.ListAliases()
	if aliases2[0].Name == "mutated" {
		t.Error("ListAliases() returned a reference to the internal registry; expected a copy")
	}
}
