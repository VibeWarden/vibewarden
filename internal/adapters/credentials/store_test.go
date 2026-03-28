package credentials_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/credentials"
	"github.com/vibewarden/vibewarden/internal/domain/generate"
)

func sampleCreds() *generate.GeneratedCredentials {
	return &generate.GeneratedCredentials{
		PostgresPassword:     "postgres_pass_32chars_xyzabcdefgh",
		KratosCookieSecret:   "cookie_secret_32chars_xyzabcdefg",
		KratosCipherSecret:   "cipher_secret_32chars_xyzabcdefg",
		GrafanaAdminPassword: "grafana_pass_24chars_xyz",
		OpenBaoDevRootToken:  "openbao_token_32chars_xyzabcdefg",
	}
}

func TestStore_Write_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	s := credentials.NewStore()
	ctx := context.Background()

	if err := s.Write(ctx, sampleCreds(), dir); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	path := filepath.Join(dir, ".credentials")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected .credentials file to exist at %q: %v", path, err)
	}
}

func TestStore_Write_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	s := credentials.NewStore()
	ctx := context.Background()

	if err := s.Write(ctx, sampleCreds(), dir); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	path := filepath.Join(dir, ".credentials")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}

	wantMode := os.FileMode(0o600)
	gotMode := info.Mode().Perm()
	if gotMode != wantMode {
		t.Errorf("file permissions = %04o, want %04o", gotMode, wantMode)
	}
}

func TestStore_Write_DotenvFormat(t *testing.T) {
	dir := t.TempDir()
	s := credentials.NewStore()
	ctx := context.Background()
	creds := sampleCreds()

	if err := s.Write(ctx, creds, dir); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	path := filepath.Join(dir, ".credentials")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	defer f.Close()

	wantKeys := map[string]string{
		"POSTGRES_PASSWORD":      creds.PostgresPassword,
		"KRATOS_SECRETS_COOKIE":  creds.KratosCookieSecret,
		"KRATOS_SECRETS_CIPHER":  creds.KratosCipherSecret,
		"GRAFANA_ADMIN_PASSWORD": creds.GrafanaAdminPassword,
		"OPENBAO_DEV_ROOT_TOKEN": creds.OpenBaoDevRootToken,
	}

	found := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			found[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning credentials file: %v", err)
	}

	for key, wantVal := range wantKeys {
		gotVal, ok := found[key]
		if !ok {
			t.Errorf("credentials file missing key %q", key)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("credentials[%q] = %q, want %q", key, gotVal, wantVal)
		}
	}
}

func TestStore_Read_ParsesCorrectly(t *testing.T) {
	dir := t.TempDir()
	s := credentials.NewStore()
	ctx := context.Background()
	original := sampleCreds()

	if err := s.Write(ctx, original, dir); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	got, err := s.Read(ctx, dir)
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"PostgresPassword", got.PostgresPassword, original.PostgresPassword},
		{"KratosCookieSecret", got.KratosCookieSecret, original.KratosCookieSecret},
		{"KratosCipherSecret", got.KratosCipherSecret, original.KratosCipherSecret},
		{"GrafanaAdminPassword", got.GrafanaAdminPassword, original.GrafanaAdminPassword},
		{"OpenBaoDevRootToken", got.OpenBaoDevRootToken, original.OpenBaoDevRootToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Read() %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestStore_Read_NotExist(t *testing.T) {
	dir := t.TempDir()
	s := credentials.NewStore()
	ctx := context.Background()

	_, err := s.Read(ctx, dir)
	if err == nil {
		t.Fatal("Read() expected error when file missing, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read() error = %v, want to wrap os.ErrNotExist", err)
	}
}

func TestStore_Read_IgnoresComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials")

	content := `# This is a comment
# Another comment

POSTGRES_PASSWORD=testpass
# inline-style comment line
KRATOS_SECRETS_COOKIE=testcookie
KRATOS_SECRETS_CIPHER=testcipher
GRAFANA_ADMIN_PASSWORD=testgrafana
OPENBAO_DEV_ROOT_TOKEN=testtoken
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := credentials.NewStore()
	ctx := context.Background()

	got, err := s.Read(ctx, dir)
	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}

	if got.PostgresPassword != "testpass" {
		t.Errorf("PostgresPassword = %q, want %q", got.PostgresPassword, "testpass")
	}
	if got.KratosCookieSecret != "testcookie" {
		t.Errorf("KratosCookieSecret = %q, want %q", got.KratosCookieSecret, "testcookie")
	}
}

func TestStore_Write_CreatesOutputDirIfMissing(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "output")
	s := credentials.NewStore()
	ctx := context.Background()

	if err := s.Write(ctx, sampleCreds(), dir); err != nil {
		t.Fatalf("Write() unexpected error when output dir missing: %v", err)
	}

	path := filepath.Join(dir, ".credentials")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected .credentials to exist after Write() with missing dir: %v", err)
	}
}
