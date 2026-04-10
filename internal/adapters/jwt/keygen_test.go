package jwt

import (
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateDevKeys_GeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: unexpected error: %v", err)
	}
	if kp == nil {
		t.Fatal("LoadOrGenerateDevKeys: returned nil key pair")
	}
	if kp.PrivateKey == nil {
		t.Fatal("LoadOrGenerateDevKeys: nil private key")
	}
	if kp.Dir != dir {
		t.Errorf("LoadOrGenerateDevKeys: Dir = %q, want %q", kp.Dir, dir)
	}

	// Private key file must exist and be mode 0600.
	privPath := filepath.Join(dir, DevPrivateKeyFile)
	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key file permissions = %o, want 600", info.Mode().Perm())
	}

	// Public key file must exist.
	pubPath := filepath.Join(dir, DevPublicKeyFile)
	if _, err := os.Stat(pubPath); err != nil {
		t.Fatalf("public key file not written: %v", err)
	}

	// Key must be RSA-2048.
	if kp.PrivateKey.N.BitLen() != 2048 {
		t.Errorf("key size = %d bits, want 2048", kp.PrivateKey.N.BitLen())
	}
}

func TestLoadOrGenerateDevKeys_LoadsExisting(t *testing.T) {
	dir := t.TempDir()

	// Generate once.
	first, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Load again — must return the same public key.
	second, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	// Compare public key moduli to verify they are the same key.
	if first.PrivateKey.N.Cmp(second.PrivateKey.N) != 0 {
		t.Error("second load returned a different key than the first generation")
	}
}

func TestLoadOrGenerateDevKeys_CreatesDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "subdir", "keys")

	_, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestLoadOrGenerateDevKeys_InvalidPEMReturnsError(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, DevPrivateKeyFile)

	// Write garbage that looks like a PEM block but is not valid.
	if err := os.WriteFile(privPath, []byte("not a valid pem file"), 0o600); err != nil {
		t.Fatalf("writing bad pem: %v", err)
	}

	_, err := LoadOrGenerateDevKeys(dir)
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func TestPublicKey_ReturnsPublicPartOfKeyPair(t *testing.T) {
	dir := t.TempDir()
	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	pub := PublicKey(kp)
	if pub == nil {
		t.Fatal("PublicKey returned nil")
	}
	_, ok := any(pub).(*rsa.PublicKey)
	if !ok {
		t.Errorf("PublicKey did not return *rsa.PublicKey")
	}
	if pub.N.Cmp(kp.PrivateKey.N) != 0 {
		t.Error("PublicKey modulus does not match private key modulus")
	}
}
