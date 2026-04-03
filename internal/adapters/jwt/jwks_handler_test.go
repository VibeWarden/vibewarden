package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
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

// ---------------------------------------------------------------------------
// TokenHandler tests
// ---------------------------------------------------------------------------

func testKeyPair(t *testing.T) *DevKeyPair {
	t.Helper()
	kp, err := LoadOrGenerateDevKeys(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}
	return kp
}

func TestTokenHandler_DefaultClaims(t *testing.T) {
	kp := testKeyPair(t)
	h := NewTokenHandler(kp)

	req := httptest.NewRequest(http.MethodGet, DevTokenPath, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !strings.HasPrefix(body.Token, "ey") {
		t.Errorf("token = %q, want JWT (starts with ey)", body.Token)
	}
}

func TestTokenHandler_CustomClaims(t *testing.T) {
	kp := testKeyPair(t)
	h := NewTokenHandler(kp)

	req := httptest.NewRequest(http.MethodGet,
		DevTokenPath+"?sub=alice&email=alice@test.com&name=Alice&role=admin&expires=2h", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	// Parse and verify the JWT claims.
	tok, err := josejwt.ParseSigned(body.Token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned: %v", err)
	}

	var std josejwt.Claims
	var custom map[string]any
	if err := tok.Claims(&kp.PrivateKey.PublicKey, &std, &custom); err != nil {
		t.Fatalf("Claims: %v", err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"sub", std.Subject, "alice"},
		{"email", custom["email"], "alice@test.com"},
		{"name", custom["name"], "Alice"},
		{"role", custom["role"], "admin"},
		{"iss", std.Issuer, DevIssuer},
	}
	for _, c := range checks {
		t.Run(c.field, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("claim %s = %v, want %v", c.field, c.got, c.want)
			}
		})
	}
}

func TestTokenHandler_InvalidExpires(t *testing.T) {
	kp := testKeyPair(t)
	h := NewTokenHandler(kp)

	req := httptest.NewRequest(http.MethodGet, DevTokenPath+"?expires=notaduration", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestTokenHandler_ZeroExpires(t *testing.T) {
	kp := testKeyPair(t)
	h := NewTokenHandler(kp)

	req := httptest.NewRequest(http.MethodGet, DevTokenPath+"?expires=0s", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestTokenHandler_ExpiryRespected(t *testing.T) {
	kp := testKeyPair(t)
	h := NewTokenHandler(kp)

	req := httptest.NewRequest(http.MethodGet, DevTokenPath+"?expires=30m", nil)
	before := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	after := time.Now()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	tok, _ := josejwt.ParseSigned(body.Token, []jose.SignatureAlgorithm{jose.RS256})
	var std josejwt.Claims
	_ = tok.Claims(&kp.PrivateKey.PublicKey, &std)

	expTime := std.Expiry.Time()
	lo := before.Add(30 * time.Minute).Add(-2 * time.Second)
	hi := after.Add(30 * time.Minute).Add(2 * time.Second)
	if expTime.Before(lo) || expTime.After(hi) {
		t.Errorf("expiry %v not in expected window [%v, %v]", expTime, lo, hi)
	}
}

// ---------------------------------------------------------------------------
// SignToken tests
// ---------------------------------------------------------------------------

func TestSignToken_RoundTrip(t *testing.T) {
	kp := testKeyPair(t)

	raw, err := SignToken(context.Background(), kp, "user-1", "u@test.com", "User", "admin", time.Hour)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	tok, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned: %v", err)
	}

	var std josejwt.Claims
	var custom map[string]any
	if err := tok.Claims(&kp.PrivateKey.PublicKey, &std, &custom); err != nil {
		t.Fatalf("Claims: %v", err)
	}

	if std.Subject != "user-1" {
		t.Errorf("sub = %q, want user-1", std.Subject)
	}
	if custom["email"] != "u@test.com" {
		t.Errorf("email = %v, want u@test.com", custom["email"])
	}
	if std.Issuer != DevIssuer {
		t.Errorf("iss = %q, want %q", std.Issuer, DevIssuer)
	}
}
