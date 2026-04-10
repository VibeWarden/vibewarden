package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DevKeyDir is the subdirectory under .vibewarden/ where dev key files are stored.
	DevKeyDir = ".vibewarden/dev-keys"

	// DevPrivateKeyFile is the file name of the RSA private key PEM file.
	DevPrivateKeyFile = "private.pem"

	// DevPublicKeyFile is the file name of the RSA public key PEM file.
	DevPublicKeyFile = "public.pem"

	devKeyBits = 2048
)

// DevKeyPair holds the RSA-2048 key pair used for local dev JWT signing.
type DevKeyPair struct {
	// PrivateKey is the RSA private key used for signing tokens.
	PrivateKey *rsa.PrivateKey

	// Dir is the directory on disk where the PEM files were loaded or created.
	Dir string
}

// LoadOrGenerateDevKeys loads the RSA-2048 key pair from dir/private.pem and
// dir/public.pem. When the files do not exist they are generated and written to
// disk so they persist across restarts.
//
// dir is the directory that will contain (or already contains) the key files.
// The directory is created with permissions 0700 if it does not exist.
//
// Returns an error if the directory cannot be created, if reading or parsing
// the PEM files fails, or if key generation fails.
func LoadOrGenerateDevKeys(dir string) (*DevKeyPair, error) {
	privPath := filepath.Join(dir, DevPrivateKeyFile)
	pubPath := filepath.Join(dir, DevPublicKeyFile)

	// Attempt to load existing keys.
	if _, err := os.Stat(privPath); err == nil {
		key, loadErr := loadPrivateKey(privPath)
		if loadErr != nil {
			return nil, fmt.Errorf("keygen: loading existing private key: %w", loadErr)
		}
		return &DevKeyPair{PrivateKey: key, Dir: dir}, nil
	}

	// Keys do not exist — generate and persist.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("keygen: creating key directory %q: %w", dir, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, devKeyBits)
	if err != nil {
		return nil, fmt.Errorf("keygen: generating RSA-%d key: %w", devKeyBits, err)
	}

	if err := writePrivateKey(privPath, key); err != nil {
		return nil, fmt.Errorf("keygen: writing private key: %w", err)
	}
	if err := writePublicKey(pubPath, &key.PublicKey); err != nil {
		return nil, fmt.Errorf("keygen: writing public key: %w", err)
	}

	return &DevKeyPair{PrivateKey: key, Dir: dir}, nil
}

// loadPrivateKey reads an RSA private key from a PEM-encoded PKCS#8 or
// PKCS#1 file at path.
func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is an internal dev-key directory constructed by LoadOrGenerateDevKeys
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing PKCS#1 key: %w", err)
		}
		return key, nil
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing PKCS#8 key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// writePrivateKey encodes key as PKCS#8 PEM and writes it to path with mode 0600.
func writePrivateKey(path string, key *rsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshalling private key: %w", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	data := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

// writePublicKey encodes pub as PKIX PEM and writes it to path with mode 0644.
func writePublicKey(path string, pub *rsa.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("marshalling public key: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	data := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // public key, world-readable is correct
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}
