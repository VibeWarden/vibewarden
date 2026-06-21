// Package builtin implements a VibeWarden SecretStore adapter that stores
// secrets in a local AES-256-GCM encrypted JSON file. It uses only Go stdlib
// crypto packages and requires no external services.
//
// The adapter encrypts the entire secret map as a single JSON blob, using a
// 32-byte master key provided via environment variable or key file. Writes are
// atomic (temp file + rename) and all operations are protected by a read-write
// mutex for concurrent safety.
package builtin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// masterKeyLen is the required length of the AES-256 master key in bytes.
const masterKeyLen = 32

// Store implements ports.SecretStore using an AES-256-GCM encrypted JSON file.
// All secrets are kept in memory and flushed to disk on every mutation.
type Store struct {
	path      string
	masterKey []byte

	mu      sync.RWMutex
	secrets map[string]map[string]string // path -> key -> value
}

// NewStore creates a new builtin secret Store. The path argument is the
// location of the encrypted secrets file (e.g. ".vibewarden/secrets.enc").
// The masterKey must be exactly 32 bytes (AES-256).
//
// NewStore does not read the file immediately; the first call to any method
// will load and decrypt the file if it exists.
func NewStore(path string, masterKey []byte) (*Store, error) {
	if len(masterKey) != masterKeyLen {
		return nil, fmt.Errorf("builtin secret store: master key must be %d bytes, got %d", masterKeyLen, len(masterKey))
	}
	if path == "" {
		return nil, errors.New("builtin secret store: path must not be empty")
	}

	s := &Store{
		path:      path,
		masterKey: masterKey,
		secrets:   make(map[string]map[string]string),
	}

	// Load existing secrets from disk if the file exists.
	if err := s.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("builtin secret store: loading secrets: %w", err)
	}

	return s, nil
}

// Get implements ports.SecretStore. It returns the key/value pairs stored at
// path. Returns ports.ErrSecretNotFound when the path does not exist.
func (s *Store) Get(_ context.Context, path string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.secrets[path]
	if !ok {
		return nil, fmt.Errorf("builtin: secret not found at %q: %w", path, ports.ErrSecretNotFound)
	}

	// Return a copy to prevent callers from mutating internal state.
	out := make(map[string]string, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out, nil
}

// Put implements ports.SecretStore. It writes data at path, creating or
// updating the secret. The encrypted file is flushed to disk atomically.
func (s *Store) Put(_ context.Context, path string, data map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy to prevent callers from mutating internal state.
	stored := make(map[string]string, len(data))
	for k, v := range data {
		stored[k] = v
	}
	s.secrets[path] = stored

	if err := s.flushToDisk(); err != nil {
		return fmt.Errorf("builtin: writing secrets file: %w", err)
	}
	return nil
}

// Delete implements ports.SecretStore. It removes the secret at path.
// The encrypted file is flushed to disk atomically.
func (s *Store) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.secrets, path)

	if err := s.flushToDisk(); err != nil {
		return fmt.Errorf("builtin: writing secrets file after delete: %w", err)
	}
	return nil
}

// List implements ports.SecretStore. It returns the keys (child paths) beneath
// the given prefix. Keys ending in "/" denote sub-directories (paths that are
// prefixes of other paths).
func (s *Store) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// prefixDir is the prefix normalised to end with "/" so that a bare
	// HasPrefix check cannot match a sibling entry: e.g. prefix "auth" must
	// not match stored path "auth-evil" (OWASP A03 prefix-extension). Callers
	// that already supply a trailing slash ("app/") are unaffected. The empty
	// prefix ("") is kept as-is so HasPrefix returns true for every path,
	// preserving the "list all top-level entries" behaviour.
	// Exact match (path == prefix) lets a stored path that equals the prefix
	// reach the rel == "" guard below and be excluded from results.
	prefixDir := prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefixDir += "/"
	}

	seen := make(map[string]struct{})
	for path := range s.secrets {
		if path != prefix && !strings.HasPrefix(path, prefixDir) {
			continue
		}

		// Strip the prefix to get the relative path.
		rel := strings.TrimPrefix(path, prefix)
		if rel == "" {
			continue
		}

		// If the relative path contains a "/", it is a sub-directory.
		// Return only the first segment with a trailing "/".
		if idx := strings.Index(rel, "/"); idx >= 0 {
			seen[rel[:idx+1]] = struct{}{}
		} else {
			seen[rel] = struct{}{}
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys, nil
}

// Health implements ports.SecretStore. It verifies that the master key is set
// and the secrets file directory is writable.
func (s *Store) Health(_ context.Context) error {
	if len(s.masterKey) != masterKeyLen {
		return errors.New("builtin: master key not configured")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("builtin: secrets directory not writable: %w", err)
	}
	return nil
}

// loadFromDisk reads and decrypts the secrets file. If the file does not exist,
// the store starts empty (not an error).
func (s *Store) loadFromDisk() error {
	ciphertext, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // empty store
		}
		return fmt.Errorf("reading secrets file: %w", err)
	}

	if len(ciphertext) == 0 {
		return nil // empty file treated as empty store
	}

	plaintext, err := s.decrypt(ciphertext)
	if err != nil {
		return fmt.Errorf("decrypting secrets file: %w", err)
	}

	var data map[string]map[string]string
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return fmt.Errorf("unmarshalling secrets: %w", err)
	}

	s.secrets = data
	return nil
}

// flushToDisk encrypts the current secrets map and writes it atomically to disk.
// It writes to a temporary file first, then renames it to the target path.
// Must be called with s.mu held for writing.
func (s *Store) flushToDisk() error {
	plaintext, err := json.Marshal(s.secrets)
	if err != nil {
		return fmt.Errorf("marshalling secrets: %w", err)
	}

	ciphertext, err := s.encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypting secrets: %w", err)
	}

	// Ensure the parent directory exists.
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating secrets directory: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpFile := s.path + ".tmp"
	if err := os.WriteFile(tmpFile, ciphertext, 0o600); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmpFile, s.path); err != nil {
		// Best-effort cleanup of the temp file.
		_ = os.Remove(tmpFile)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// encrypt encrypts plaintext using AES-256-GCM with a random nonce.
// The output format is: nonce || ciphertext (nonce is prepended).
func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt decrypts ciphertext that was encrypted by encrypt.
// It expects the format: nonce || ciphertext.
func (s *Store) decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	return plaintext, nil
}

// ResolveMasterKey resolves the 32-byte AES-256 master key from the config.
// It checks the key file first, then falls back to the VIBEWARDEN_SECRETS_MASTER_KEY
// environment variable. This function is shared between the secrets plugin
// and the CLI commands to avoid duplication.
func ResolveMasterKey(keyFile string) ([]byte, error) {
	var hexKey string

	if keyFile != "" {
		raw, err := os.ReadFile(keyFile) //nolint:gosec // keyFile is from trusted config
		if err != nil {
			return nil, fmt.Errorf("reading key file %q: %w", keyFile, err)
		}
		hexKey = strings.TrimSpace(string(raw))
	} else {
		hexKey = os.Getenv("VIBEWARDEN_SECRETS_MASTER_KEY")
	}

	if hexKey == "" {
		return nil, fmt.Errorf("master key not set: set VIBEWARDEN_SECRETS_MASTER_KEY or configure secrets.builtin.key_file")
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decoding hex master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return key, nil
}

// Interface guard — compile-time verification that Store implements SecretStore.
var _ ports.SecretStore = (*Store)(nil)
