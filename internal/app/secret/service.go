// Package secret provides the application service for retrieving secrets.
// It orchestrates the OpenBao adapter and the credentials file adapter,
// implementing OpenBao-first retrieval with a .credentials file fallback.
package secret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	domainsecret "github.com/vibewarden/vibewarden/internal/domain/secret"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrSecretNotFound is returned when a secret cannot be found in any source.
var ErrSecretNotFound = errors.New("secret not found")

// ErrNoSourceAvailable is returned when neither OpenBao nor the credentials
// file is available.
var ErrNoSourceAvailable = errors.New("no secret source available: OpenBao is not running and .credentials file not found")

// Service implements secret retrieval with OpenBao-first, credentials-file fallback.
// It satisfies the ports.SecretRetriever interface.
type Service struct {
	secretStore ports.SecretStore // may be nil if OpenBao is not configured
	credStore   ports.CredentialStore
	outputDir   string // directory containing .credentials
}

// NewService creates a secret retrieval service.
// secretStore may be nil; in that case, only the credentials file is used.
func NewService(
	secretStore ports.SecretStore,
	credStore ports.CredentialStore,
	outputDir string,
) *Service {
	return &Service{
		secretStore: secretStore,
		credStore:   credStore,
		outputDir:   outputDir,
	}
}

// Get retrieves a secret by alias or path.
//
// Resolution order:
//  1. Check if aliasOrPath is a well-known alias.
//  2. Try OpenBao (dynamic creds first for postgres, then static path).
//  3. Fall back to .credentials file (only for well-known aliases).
//
// For arbitrary (non-alias) paths, OpenBao is required. Returns
// ErrNoSourceAvailable when OpenBao is not running and path has no alias.
func (s *Service) Get(ctx context.Context, aliasOrPath string) (*domainsecret.RetrievedSecret, error) {
	alias := domainsecret.ResolveAlias(aliasOrPath)

	// Determine the OpenBao static path to query.
	openBaoPath := aliasOrPath
	if alias != nil && alias.OpenBaoPath != "" {
		openBaoPath = alias.OpenBaoPath
	}

	// For the "openbao" alias (or any alias with no OpenBaoPath), skip OpenBao entirely.
	// For arbitrary paths and all other aliases, try OpenBao first.
	skipOpenBao := alias != nil && alias.OpenBaoPath == ""
	if !skipOpenBao {
		data, err := s.tryOpenBao(ctx, alias, openBaoPath)
		if err != nil {
			return nil, fmt.Errorf("openbao retrieval: %w", err)
		}
		if data != nil {
			result := &domainsecret.RetrievedSecret{
				Path:   openBaoPath,
				Source: domainsecret.SourceOpenBao,
				Data:   data,
			}
			if alias != nil {
				result.Alias = alias.Name
			}
			return result, nil
		}
	}

	// No alias means it is an arbitrary path — cannot fall back to .credentials.
	if alias == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoSourceAvailable, aliasOrPath)
	}

	// Try .credentials fallback.
	data, err := s.tryCredentialsFile(ctx, alias)
	if err != nil {
		return nil, err
	}
	if data != nil {
		return &domainsecret.RetrievedSecret{
			Path:   aliasOrPath,
			Alias:  alias.Name,
			Source: domainsecret.SourceCredentialsFile,
			Data:   data,
		}, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrNoSourceAvailable, aliasOrPath)
}

// List returns all managed secret paths from both sources.
// It always includes all well-known alias paths, and when OpenBao is available
// it also includes paths listed under "infra/" and "app/" prefixes.
// The result is deduplicated and sorted.
func (s *Service) List(ctx context.Context) ([]string, error) {
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// Always include well-known alias names.
	for _, a := range domainsecret.ListAliases() {
		add(a.Name)
	}

	// If OpenBao is available, also include paths from the store.
	if s.secretStore != nil {
		if err := s.secretStore.Health(ctx); err == nil {
			for _, prefix := range []string{"infra/", "app/"} {
				keys, err := s.secretStore.List(ctx, prefix)
				if err != nil {
					// Non-fatal: OpenBao may have an empty prefix.
					continue
				}
				for _, k := range keys {
					add(prefix + k)
				}
			}
		}
	}

	sort.Strings(paths)
	return paths, nil
}

// tryOpenBao attempts to retrieve a secret from OpenBao.
// Returns nil, nil when OpenBao is not configured, not available, or the path
// is empty (e.g. the "openbao" alias has no OpenBao path).
// Returns nil, nil on health-check failure (triggering the fallback).
func (s *Service) tryOpenBao(ctx context.Context, alias *domainsecret.WellKnownAlias, path string) (map[string]string, error) {
	if s.secretStore == nil {
		return nil, nil
	}
	if path == "" {
		return nil, nil
	}

	// Health check before attempting retrieval.
	if err := s.secretStore.Health(ctx); err != nil {
		return nil, nil // not available — trigger fallback
	}

	// For aliases with a dynamic role, try dynamic credentials first.
	if alias != nil && alias.DynamicRole != "" {
		data, err := s.tryDynamicCredentials(ctx, alias.DynamicRole)
		if err == nil && data != nil {
			return data, nil
		}
		// Dynamic failed — fall through to static path.
	}

	data, err := s.secretStore.Get(ctx, path)
	if err != nil {
		// Path not found in OpenBao is not an error — return nil to trigger fallback.
		return nil, nil
	}
	return data, nil
}

// tryDynamicCredentials requests dynamic database credentials from OpenBao.
// Returns nil, nil when the request fails (allows fallback to static path).
func (s *Service) tryDynamicCredentials(ctx context.Context, role string) (map[string]string, error) {
	dynPath := "database/creds/" + role
	data, err := s.secretStore.Get(ctx, dynPath)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

// tryCredentialsFile attempts to retrieve a secret from the .credentials file.
// Returns nil, nil when the file does not exist.
// Returns an error for genuine I/O failures.
func (s *Service) tryCredentialsFile(ctx context.Context, alias *domainsecret.WellKnownAlias) (map[string]string, error) {
	if alias == nil || len(alias.CredentialsFileKeys) == 0 {
		return nil, nil
	}

	creds, err := s.credStore.Read(ctx, s.outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading credentials file: %w", err)
	}

	// Build the credential map from the file values using the alias key mapping.
	credMap := map[string]string{
		"POSTGRES_PASSWORD":      creds.PostgresPassword,
		"KRATOS_SECRETS_COOKIE":  creds.KratosCookieSecret,
		"KRATOS_SECRETS_CIPHER":  creds.KratosCipherSecret,
		"GRAFANA_ADMIN_PASSWORD": creds.GrafanaAdminPassword,
		"OPENBAO_DEV_ROOT_TOKEN": creds.OpenBaoDevRootToken,
	}

	out := make(map[string]string, len(alias.CredentialsFileKeys))
	for fileKey, outputName := range alias.CredentialsFileKeys {
		if v, ok := credMap[fileKey]; ok && v != "" {
			out[outputName] = v
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
