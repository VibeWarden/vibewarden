package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	domjwks "github.com/vibewarden/vibewarden/internal/domain/jwks"
)

// testLogger returns a discard logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// generateTestRSAKey generates a 2048-bit RSA key pair for testing.
func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return k
}

// signRS256Token creates a signed RS256 JWT with the given claims and key.
func signRS256Token(t *testing.T, key *rsa.PrivateKey, kid string, claims josejwt.Claims, extra map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, opts)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	raw, err := josejwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("serialize token: %v", err)
	}
	return raw
}

// fakeFetcher is a simple in-memory JWKSFetcher for unit testing.
type fakeFetcher struct {
	keys []jose.JSONWebKey
	err  error
}

func (f *fakeFetcher) FetchKeys(_ context.Context) (*domjwks.KeySet, error) {
	if f.err != nil {
		return nil, f.err
	}
	ks := &domjwks.KeySet{}
	for _, k := range f.keys {
		ks.Keys = append(ks.Keys, domjwks.Key{KID: k.KeyID, Algorithm: k.Algorithm, PublicKey: k.Key})
	}
	return ks, nil
}

func (f *fakeFetcher) GetKey(_ context.Context, kid string) (*domjwks.Key, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, k := range f.keys {
		if k.KeyID == kid {
			return &domjwks.Key{KID: k.KeyID, Algorithm: k.Algorithm, PublicKey: k.Key}, nil
		}
	}
	return nil, fmt.Errorf("jwks: key not found: %s", kid)
}

func TestAdapter_Name(t *testing.T) {
	rsaKey := generateTestRSAKey(t)
	fetcher := &fakeFetcher{keys: []jose.JSONWebKey{
		{Key: &rsaKey.PublicKey, KeyID: "key1", Algorithm: "RS256", Use: "sig"},
	}}
	a, err := NewAdapter(Config{
		JWKSURL:  "https://example.com/jwks",
		Issuer:   "https://example.com/",
		Audience: "my-api",
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if got := a.Name(); got != "jwt" {
		t.Errorf("Name() = %q, want %q", got, "jwt")
	}
}

func TestNewAdapter_Validation(t *testing.T) {
	fetcher := &fakeFetcher{}
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid config with jwks_url",
			cfg:     Config{JWKSURL: "https://example.com/jwks", Issuer: "https://example.com/", Audience: "api"},
			wantErr: false,
		},
		{
			name:    "valid config with issuer_url",
			cfg:     Config{IssuerURL: "https://example.com/", Issuer: "https://example.com/", Audience: "api"},
			wantErr: false,
		},
		{
			name:    "missing jwks_url and issuer_url",
			cfg:     Config{Issuer: "https://example.com/", Audience: "api"},
			wantErr: true,
		},
		{
			name:    "missing issuer",
			cfg:     Config{JWKSURL: "https://example.com/jwks", Audience: "api"},
			wantErr: true,
		},
		{
			name:    "missing audience",
			cfg:     Config{JWKSURL: "https://example.com/jwks", Issuer: "https://example.com/"},
			wantErr: true,
		},
		{
			name:    "nil fetcher",
			cfg:     Config{JWKSURL: "https://example.com/jwks", Issuer: "https://example.com/", Audience: "api"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil fetcher" {
				// Pass a true interface nil (not a typed nil pointer) to test the nil guard.
				_, err := NewAdapter(tt.cfg, nil, testLogger())
				if (err != nil) != tt.wantErr {
					t.Errorf("NewAdapter() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			_, err := NewAdapter(tt.cfg, fetcher, testLogger())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAdapter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_Authenticate(t *testing.T) {
	rsaKey := generateTestRSAKey(t)
	const kid = "test-key-1"
	const issuer = "https://auth.example.com/"
	const audience = "my-api"

	fetcher := &fakeFetcher{keys: []jose.JSONWebKey{
		{Key: &rsaKey.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"},
	}}

	adapter, err := NewAdapter(Config{
		JWKSURL:  "https://auth.example.com/jwks",
		Issuer:   issuer,
		Audience: audience,
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	now := time.Now()
	validClaims := josejwt.Claims{
		Subject:  "user-123",
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	}

	tests := []struct {
		name       string
		setupReq   func() *http.Request
		wantAuth   bool
		wantReason string
	}{
		{
			name: "valid RS256 token",
			setupReq: func() *http.Request {
				token := signRS256Token(t, rsaKey, kid, validClaims, map[string]any{
					"email":          "user@example.com",
					"email_verified": true,
				})
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			wantAuth: true,
		},
		{
			name: "no Authorization header",
			setupReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/api/data", nil)
			},
			wantAuth:   false,
			wantReason: "no_credentials",
		},
		{
			name: "non-Bearer Authorization",
			setupReq: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
				return req
			},
			wantAuth:   false,
			wantReason: "no_credentials",
		},
		{
			name: "empty Bearer token",
			setupReq: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer ")
				return req
			},
			wantAuth:   false,
			wantReason: "no_credentials",
		},
		{
			name: "malformed token",
			setupReq: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer not-a-jwt")
				return req
			},
			wantAuth:   false,
			wantReason: "invalid_token",
		},
		{
			name: "expired token",
			setupReq: func() *http.Request {
				expired := validClaims
				expired.Expiry = josejwt.NewNumericDate(now.Add(-2 * time.Hour))
				expired.IssuedAt = josejwt.NewNumericDate(now.Add(-3 * time.Hour))
				token := signRS256Token(t, rsaKey, kid, expired, nil)
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			wantAuth:   false,
			wantReason: "token_expired",
		},
		{
			name: "wrong issuer",
			setupReq: func() *http.Request {
				wrongIssuer := validClaims
				wrongIssuer.Issuer = "https://evil.com/"
				token := signRS256Token(t, rsaKey, kid, wrongIssuer, nil)
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			wantAuth:   false,
			wantReason: "invalid_issuer",
		},
		{
			name: "wrong audience",
			setupReq: func() *http.Request {
				wrongAud := validClaims
				wrongAud.Audience = josejwt.Audience{"other-api"}
				token := signRS256Token(t, rsaKey, kid, wrongAud, nil)
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			wantAuth:   false,
			wantReason: "invalid_audience",
		},
		{
			name: "unknown key ID",
			setupReq: func() *http.Request {
				token := signRS256Token(t, rsaKey, "unknown-kid", validClaims, nil)
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			wantAuth:   false,
			wantReason: "invalid_signature",
		},
		{
			name: "fetcher returns error",
			setupReq: func() *http.Request {
				// Use adapter with error fetcher
				token := signRS256Token(t, rsaKey, kid, validClaims, nil)
				req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				return req
			},
			// Handled separately below; this entry is a placeholder.
			wantAuth:   false,
			wantReason: "provider_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := adapter
			if tt.name == "fetcher returns error" {
				errAdapter, err := NewAdapter(Config{
					JWKSURL:  "https://auth.example.com/jwks",
					Issuer:   issuer,
					Audience: audience,
				}, &fakeFetcher{err: fmt.Errorf("network error")}, testLogger())
				if err != nil {
					t.Fatalf("NewAdapter: %v", err)
				}
				a = errAdapter
			}

			req := tt.setupReq()
			result := a.Authenticate(context.Background(), req)

			if result.Authenticated != tt.wantAuth {
				t.Errorf("Authenticated = %v, want %v (reason: %q)", result.Authenticated, tt.wantAuth, result.Reason)
			}
			if !tt.wantAuth && result.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", result.Reason, tt.wantReason)
			}
		})
	}
}

func TestAdapter_Authenticate_ClaimsMapping(t *testing.T) {
	rsaKey := generateTestRSAKey(t)
	const kid = "key-claims"
	const issuer = "https://auth.example.com/"
	const audience = "my-api"

	fetcher := &fakeFetcher{keys: []jose.JSONWebKey{
		{Key: &rsaKey.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"},
	}}
	adapter, err := NewAdapter(Config{
		JWKSURL:  "https://auth.example.com/jwks",
		Issuer:   issuer,
		Audience: audience,
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	now := time.Now()
	claims := josejwt.Claims{
		Subject:  "user-456",
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	}
	extra := map[string]any{
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice Smith",
		"roles":          []any{"admin", "user"},
	}

	token := signRS256Token(t, rsaKey, kid, claims, extra)
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	result := adapter.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("expected authenticated, got reason=%q message=%q", result.Reason, result.Message)
	}

	ident := result.Identity
	if ident.ID() != "user-456" {
		t.Errorf("Identity.ID() = %q, want %q", ident.ID(), "user-456")
	}
	if ident.Email() != "alice@example.com" {
		t.Errorf("Identity.Email() = %q, want %q", ident.Email(), "alice@example.com")
	}
	if !ident.EmailVerified() {
		t.Error("Identity.EmailVerified() = false, want true")
	}
	if ident.Provider() != "jwt" {
		t.Errorf("Identity.Provider() = %q, want %q", ident.Provider(), "jwt")
	}

	// Non-reserved custom claim should be present.
	if ident.Claim("name") != "Alice Smith" {
		t.Errorf("Identity.Claim(name) = %v, want %q", ident.Claim("name"), "Alice Smith")
	}

	// Reserved claims must NOT be present in Claims().
	for _, reserved := range []string{"sub", "iss", "aud", "exp", "iat", "nbf"} {
		if ident.HasClaim(reserved) {
			t.Errorf("reserved claim %q should not appear in Identity.Claims()", reserved)
		}
	}
}

func TestAdapter_DefaultAlgorithms(t *testing.T) {
	fetcher := &fakeFetcher{}
	a, err := NewAdapter(Config{
		JWKSURL:  "https://example.com/jwks",
		Issuer:   "https://example.com/",
		Audience: "api",
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if len(a.config.AllowedAlgorithms) != 2 {
		t.Errorf("AllowedAlgorithms length = %d, want 2", len(a.config.AllowedAlgorithms))
	}
	if a.config.AllowedAlgorithms[0] != "RS256" || a.config.AllowedAlgorithms[1] != "ES256" {
		t.Errorf("AllowedAlgorithms = %v, want [RS256 ES256]", a.config.AllowedAlgorithms)
	}
}

func TestAdapter_DefaultCacheTTL(t *testing.T) {
	fetcher := &fakeFetcher{}
	a, err := NewAdapter(Config{
		JWKSURL:  "https://example.com/jwks",
		Issuer:   "https://example.com/",
		Audience: "api",
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if a.config.CacheTTL != time.Hour {
		t.Errorf("CacheTTL = %v, want 1h", a.config.CacheTTL)
	}
}

func TestAdapter_ClaimsToHeaders(t *testing.T) {
	fetcher := &fakeFetcher{}
	mapping := map[string]string{
		"name":  "X-User-Name",
		"roles": "X-User-Roles",
	}
	a, err := NewAdapter(Config{
		JWKSURL:         "https://example.com/jwks",
		Issuer:          "https://example.com/",
		Audience:        "api",
		ClaimsToHeaders: mapping,
	}, fetcher, testLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	got := a.ClaimsToHeaders()
	if len(got) != len(mapping) {
		t.Errorf("ClaimsToHeaders() length = %d, want %d", len(got), len(mapping))
	}
	for claim, header := range mapping {
		if got[claim] != header {
			t.Errorf("ClaimsToHeaders()[%q] = %q, want %q", claim, got[claim], header)
		}
	}
}
