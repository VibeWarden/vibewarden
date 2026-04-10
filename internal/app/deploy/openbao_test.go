package deploy_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
)

// TestParseEnvReader tests the internal env-file parser via the public
// Bootstrap path. We test it directly by constructing known inputs.

func TestParseInitOutput_Valid(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantUnsealKey string
		wantRootToken string
		wantErr       bool
	}{
		{
			name: "clean JSON",
			input: `{
  "unseal_keys_b64": ["abc123=="],
  "root_token": "hvs.roottoken"
}`,
			wantUnsealKey: "abc123==",
			wantRootToken: "hvs.roottoken",
		},
		{
			name: "JSON with leading warning text",
			input: `WARNING: some warning here
{
  "unseal_keys_b64": ["xyz789=="],
  "root_token": "hvs.othertoken"
}`,
			wantUnsealKey: "xyz789==",
			wantRootToken: "hvs.othertoken",
		},
		{
			name:    "no JSON",
			input:   "something went wrong",
			wantErr: true,
		},
		{
			name:    "empty unseal keys",
			input:   `{"unseal_keys_b64": [], "root_token": "tok"}`,
			wantErr: true,
		},
		{
			name:    "empty root token",
			input:   `{"unseal_keys_b64": ["key1"], "root_token": ""}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsealKey, rootToken, err := deployapp.ParseInitOutput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseInitOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if unsealKey != tt.wantUnsealKey {
				t.Errorf("unsealKey = %q, want %q", unsealKey, tt.wantUnsealKey)
			}
			if rootToken != tt.wantRootToken {
				t.Errorf("rootToken = %q, want %q", rootToken, tt.wantRootToken)
			}
		})
	}
}

func TestParseEnvReader_Valid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "simple key-value",
			input: "FOO=bar\nBAZ=qux\n",
			want:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:  "comments and blank lines ignored",
			input: "# comment\n\nFOO=bar\n",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "double-quoted value",
			input: `DB_URL="postgres://localhost/mydb"`,
			want:  map[string]string{"DB_URL": "postgres://localhost/mydb"},
		},
		{
			name:  "single-quoted value",
			input: `SECRET='my secret value'`,
			want:  map[string]string{"SECRET": "my secret value"},
		},
		{
			name:  "value with equals sign",
			input: "TOKEN=abc=def=",
			want:  map[string]string{"TOKEN": "abc=def="},
		},
		{
			name:  "empty value",
			input: "EMPTY=",
			want:  map[string]string{"EMPTY": ""},
		},
		{
			name:    "missing equals separator",
			input:   "NOEQUALS\n",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=value\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deployapp.ParseEnvReader(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseEnvReader() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("ParseEnvReader()[%q] = %q, want %q", k, got[k], v)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("ParseEnvReader() len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain value", "hello", "'hello'"},
		{"value with space", "hello world", "'hello world'"},
		{"value with single quote", "it's fine", `'it'\''s fine'`},
		{"empty string", "", "''"},
		{"value with dollar", "$SECRET", "'$SECRET'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deployapp.ShellQuote(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBootstrapper_Bootstrap_FirstTime verifies that on a fresh OpenBao instance
// the bootstrapper runs the full init sequence.
func TestBootstrapper_Bootstrap_FirstTime(t *testing.T) {
	initJSON := `{"unseal_keys_b64":["dGVzdHVuc2VhbGtleQ=="],"root_token":"hvs.testroot"}`

	wrappedExec := &wildcardExecutor{
		wildcards: map[string]runResponse{
			// isInitialized: bao init -status exits 2 (not initialized)
			"bao operator init -status": {
				output: "Vault is not initialized",
				err:    errors.New("exit status 2"),
			},
			// operator init (no token needed)
			"bao operator init -key-shares=1 -key-threshold=1 -format=json": {
				output: initJSON,
			},
			// operator unseal
			"bao operator unseal dGVzdHVuc2VhbGtleQ==": {},
			// secrets enable kv-v2
			"bao secrets enable -path=secret kv-v2": {},
			// auth enable approle
			"bao auth enable approle": {},
			// policy write
			"bao policy write": {},
			// create role
			"bao write auth/approle/role/vibewarden token_policies": {},
			// read role_id
			"bao read -field=role_id auth/approle/role/vibewarden/role-id": {
				output: "test-role-id",
			},
			// generate secret_id
			"bao write -field=secret_id -f auth/approle/role/vibewarden/secret-id": {
				output: "test-secret-id",
			},
		},
	}

	bootstrapper := deployapp.NewOpenBaoBootstrapper(wrappedExec)

	result, err := bootstrapper.Bootstrap(context.Background(), deployapp.BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	if result.UnsealKey == "" {
		t.Error("expected UnsealKey to be set after first-time bootstrap")
	}
	if result.RootToken == "" {
		t.Error("expected RootToken to be set after first-time bootstrap")
	}
	if result.RoleID == "" {
		t.Error("expected RoleID to be set after first-time bootstrap")
	}
	if result.SecretID == "" {
		t.Error("expected SecretID to be set after first-time bootstrap")
	}
}

// TestBootstrapper_Bootstrap_AlreadyInitialisedUnsealed verifies that on
// subsequent deploys the bootstrapper skips the init sequence.
func TestBootstrapper_Bootstrap_AlreadyInitialisedUnsealed(t *testing.T) {
	exec := &wildcardExecutor{
		wildcards: map[string]runResponse{
			// isInitialized: exits 0 (already initialized)
			"bao operator init -status": {output: "Vault is initialized"},
			// isSealed: status returns unsealed JSON
			"bao status -format=json": {output: `{"sealed":false}`},
		},
	}

	bootstrapper := deployapp.NewOpenBaoBootstrapper(exec)

	result, err := bootstrapper.Bootstrap(context.Background(), deployapp.BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap() unexpected error: %v", err)
	}

	// On subsequent deploy, UnsealKey should be empty (not returned again).
	if result.UnsealKey != "" {
		t.Errorf("expected empty UnsealKey on subsequent deploy, got %q", result.UnsealKey)
	}
}

// TestBootstrapper_Bootstrap_AlreadyInitialisedSealed verifies that the
// bootstrapper unseals when sealed and an unseal key is provided.
func TestBootstrapper_Bootstrap_AlreadyInitialisedSealed(t *testing.T) {
	exec := &wildcardExecutor{
		wildcards: map[string]runResponse{
			"bao operator init -status": {output: "Vault is initialized"},
			"bao status -format=json":   {output: `{"sealed":true}`, err: errors.New("exit status 2")},
			"bao operator unseal":       {output: "Unseal Key (will be hidden):"},
		},
	}

	bootstrapper := deployapp.NewOpenBaoBootstrapper(exec)

	var buf strings.Builder
	result, err := bootstrapper.Bootstrap(context.Background(), deployapp.BootstrapOptions{
		UnsealKey: "myunsealkey",
		Out:       &buf,
	})
	if err != nil {
		t.Fatalf("Bootstrap() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	output := buf.String()
	if !strings.Contains(output, "unsealed") {
		t.Errorf("expected 'unsealed' in output, got: %q", output)
	}
}

// TestBootstrapper_Bootstrap_SealedNoUnsealKey verifies that an error is
// returned when OpenBao is sealed and no unseal key is provided.
func TestBootstrapper_Bootstrap_SealedNoUnsealKey(t *testing.T) {
	exec := &wildcardExecutor{
		wildcards: map[string]runResponse{
			"bao operator init -status": {output: "Vault is initialized"},
			"bao status -format=json":   {output: `{"sealed":true}`, err: errors.New("exit status 2")},
		},
	}

	bootstrapper := deployapp.NewOpenBaoBootstrapper(exec)

	_, err := bootstrapper.Bootstrap(context.Background(), deployapp.BootstrapOptions{
		UnsealKey: "", // not provided
	})
	if err == nil {
		t.Fatal("expected error when sealed and no unseal key provided")
	}
	if !strings.Contains(err.Error(), "unseal key") {
		t.Errorf("error should mention 'unseal key', got: %v", err)
	}
}

// TestBootstrapper_Bootstrap_InitFails verifies error propagation when
// `bao operator init` fails.
func TestBootstrapper_Bootstrap_InitFails(t *testing.T) {
	exec := &wildcardExecutor{
		wildcards: map[string]runResponse{
			"bao operator init -status": {
				output: "Vault is not initialized",
				err:    errors.New("exit status 2"),
			},
			"bao operator init -key-shares=1": {
				err: errors.New("connection refused"),
			},
		},
	}

	bootstrapper := deployapp.NewOpenBaoBootstrapper(exec)

	_, err := bootstrapper.Bootstrap(context.Background(), deployapp.BootstrapOptions{})
	if err == nil {
		t.Fatal("expected error when operator init fails")
	}
	if !strings.Contains(err.Error(), "operator init") {
		t.Errorf("error should mention 'operator init', got: %v", err)
	}
}

// wildcardExecutor is a RemoteExecutor that matches commands by substring
// prefix, falling back to inner if set.
type wildcardExecutor struct {
	inner     *fakeExecutor
	wildcards map[string]runResponse
	runCalls  []string
}

func (w *wildcardExecutor) Run(_ context.Context, cmd string) (string, error) {
	w.runCalls = append(w.runCalls, cmd)
	for prefix, resp := range w.wildcards {
		if strings.Contains(cmd, prefix) {
			return resp.output, resp.err
		}
	}
	if w.inner != nil {
		if r, ok := w.inner.runResponses[cmd]; ok {
			return r.output, r.err
		}
	}
	return "", nil
}

func (w *wildcardExecutor) RunStream(_ context.Context, _ string, _ io.Writer, _ io.Writer) error {
	return nil
}

func (w *wildcardExecutor) Transfer(_ context.Context, _, _ string, _ bool) error {
	return nil
}

func (w *wildcardExecutor) TransferFile(_ context.Context, _, _ string) error {
	return nil
}
