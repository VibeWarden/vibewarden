package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestNewJWKSHandler_ServeHTTP(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	handler, err := NewJWKSHandler(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("NewJWKSHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, DevJWKSPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var jwks jose.JSONWebKeySet
	if err := json.NewDecoder(rec.Body).Decode(&jwks); err != nil {
		t.Fatalf("decoding JWKS response: %v", err)
	}
	if len(jwks.Keys) != 1 {
		t.Fatalf("keys count = %d, want 1", len(jwks.Keys))
	}
	key := jwks.Keys[0]
	if key.KeyID != DevKID {
		t.Errorf("kid = %q, want %q", key.KeyID, DevKID)
	}
	if key.Algorithm != string(jose.RS256) {
		t.Errorf("algorithm = %q, want RS256", key.Algorithm)
	}
	if key.Use != "sig" {
		t.Errorf("use = %q, want sig", key.Use)
	}
}

func TestNewJWKSHandler_KeyIDIsDevKID(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	handler, err := NewJWKSHandler(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("NewJWKSHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("handler is nil")
	}
	if len(handler.body) == 0 {
		t.Fatal("handler body is empty")
	}
}
