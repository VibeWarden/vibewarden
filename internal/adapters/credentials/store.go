package credentials

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/generate"
)

const (
	credentialsFileName = ".credentials"
	// permSecretFile is the permission mode for the credentials file.
	// Only owner can read/write — group and world have no access.
	permSecretFile = os.FileMode(0o600)
	permDir        = os.FileMode(0o750)
)

// Store implements ports.CredentialStore using a dotenv-formatted file.
type Store struct{}

// NewStore creates a Store.
func NewStore() *Store {
	return &Store{}
}

// Write persists credentials to .credentials in dotenv format.
// The file is created with mode 0600 (owner read/write only).
func (s *Store) Write(_ context.Context, creds *generate.GeneratedCredentials, outputDir string) error {
	if err := os.MkdirAll(outputDir, permDir); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	path := filepath.Join(outputDir, credentialsFileName)

	content := fmt.Sprintf(`# Generated credentials — do not commit to version control.
# Re-run 'vibewarden generate' to regenerate with fresh values.
# Mode: 0600 (owner read/write only)

POSTGRES_PASSWORD=%s
KRATOS_SECRETS_COOKIE=%s
KRATOS_SECRETS_CIPHER=%s
GRAFANA_ADMIN_PASSWORD=%s
OPENBAO_DEV_ROOT_TOKEN=%s
`, creds.PostgresPassword, creds.KratosCookieSecret, creds.KratosCipherSecret,
		creds.GrafanaAdminPassword, creds.OpenBaoDevRootToken)

	if err := os.WriteFile(path, []byte(content), permSecretFile); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}

	return nil
}

// Read loads credentials from the .credentials file in outputDir.
// Returns os.ErrNotExist wrapped in the error chain when the file is missing.
func (s *Store) Read(_ context.Context, outputDir string) (*generate.GeneratedCredentials, error) {
	path := filepath.Join(outputDir, credentialsFileName)

	file, err := os.Open(path) //nolint:gosec // path is constructed from trusted config-provided outputDir via filepath.Join
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }() //nolint:errcheck // read-only file close error is not actionable

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading credentials file: %w", err)
	}

	return &generate.GeneratedCredentials{
		PostgresPassword:     values["POSTGRES_PASSWORD"],
		KratosCookieSecret:   values["KRATOS_SECRETS_COOKIE"],
		KratosCipherSecret:   values["KRATOS_SECRETS_CIPHER"],
		GrafanaAdminPassword: values["GRAFANA_ADMIN_PASSWORD"],
		OpenBaoDevRootToken:  values["OPENBAO_DEV_ROOT_TOKEN"],
	}, nil
}
