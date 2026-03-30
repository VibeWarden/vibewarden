package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestLocalJWKSFetcher_FetchKeys(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	fetcher := NewLocalJWKSFetcher(&privKey.PublicKey)
	ks, err := fetcher.FetchKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchKeys: %v", err)
	}
	if len(ks.Keys) != 1 {
		t.Fatalf("keys count = %d, want 1", len(ks.Keys))
	}
	if ks.Keys[0].KID != DevKID {
		t.Errorf("KID = %q, want %q", ks.Keys[0].KID, DevKID)
	}
}

func TestLocalJWKSFetcher_GetKey_Found(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	fetcher := NewLocalJWKSFetcher(&privKey.PublicKey)
	key, err := fetcher.GetKey(context.Background(), DevKID)
	if err != nil {
		t.Fatalf("GetKey(%q): %v", DevKID, err)
	}
	if key == nil {
		t.Fatal("GetKey returned nil key")
	}
	if key.KID != DevKID {
		t.Errorf("KID = %q, want %q", key.KID, DevKID)
	}
}

func TestLocalJWKSFetcher_GetKey_NotFound(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	fetcher := NewLocalJWKSFetcher(&privKey.PublicKey)
	_, err = fetcher.GetKey(context.Background(), "nonexistent-kid")
	if err == nil {
		t.Fatal("expected error for unknown kid, got nil")
	}
	// Must satisfy isKeyNotFoundError so that the JWT adapter returns
	// "invalid_signature" instead of "provider_unavailable".
	if !isKeyNotFoundError(err) {
		t.Errorf("error %q should be recognised as key-not-found", err.Error())
	}
}

func TestLocalJWKSFetcher_FetchKeys_IsDeterministic(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	fetcher := NewLocalJWKSFetcher(&privKey.PublicKey)

	ks1, err := fetcher.FetchKeys(context.Background())
	if err != nil {
		t.Fatalf("first FetchKeys: %v", err)
	}
	ks2, err := fetcher.FetchKeys(context.Background())
	if err != nil {
		t.Fatalf("second FetchKeys: %v", err)
	}

	if ks1 != ks2 {
		t.Error("FetchKeys returned different key set pointers — expected same in-memory object")
	}
}
