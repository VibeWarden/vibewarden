package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
)

func integrationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// jwksStore is a concurrency-safe in-memory JWKS store used by mock servers.
type jwksStore struct {
	mu   sync.RWMutex
	jwks jose.JSONWebKeySet
}

func (s *jwksStore) set(jwks jose.JSONWebKeySet) {
	s.mu.Lock()
	s.jwks = jwks
	s.mu.Unlock()
}

func (s *jwksStore) get() jose.JSONWebKeySet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jwks
}

// newMockJWKSServer starts a test JWKS server serving the provided key set.
// It returns the server and an update function for key rotation tests.
func newMockJWKSServer(t *testing.T, initial jose.JSONWebKeySet) (*httptest.Server, func(jose.JSONWebKeySet)) {
	t.Helper()
	store := &jwksStore{jwks: initial}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.get()) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv, store.set
}

// buildTestAdapter creates a JWT adapter wired to the given JWKS server URL.
func buildTestAdapter(t *testing.T, jwksURL, issuer, audience string) *jwtadapter.Adapter {
	t.Helper()
	fetcher := jwtadapter.NewHTTPJWKSFetcher(jwksURL, 5*time.Second, time.Hour, integrationLogger())
	a, err := jwtadapter.NewAdapter(jwtadapter.Config{
		JWKSURL:  jwksURL,
		Issuer:   issuer,
		Audience: audience,
	}, fetcher, integrationLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a
}

func makeRS256JWKS(t *testing.T, key *rsa.PrivateKey, kid string) jose.JSONWebKeySet {
	t.Helper()
	return jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"},
		},
	}
}

func makeES256JWKS(t *testing.T, key *ecdsa.PrivateKey, kid string) jose.JSONWebKeySet {
	t.Helper()
	return jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: kid, Algorithm: "ES256", Use: "sig"},
		},
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims josejwt.Claims, extra map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid)
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, opts)
	if err != nil {
		t.Fatalf("create RS256 signer: %v", err)
	}
	raw, err := josejwt.Signed(sig).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("serialize RS256 token: %v", err)
	}
	return raw
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, claims josejwt.Claims) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid)
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, opts)
	if err != nil {
		t.Fatalf("create ES256 signer: %v", err)
	}
	raw, err := josejwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("serialize ES256 token: %v", err)
	}
	return raw
}

func TestIntegration_RS256_ValidToken(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	const (
		kid      = "rs256-key"
		issuer   = "https://auth.example.com/"
		audience = "integration-test"
	)

	srv, _ := newMockJWKSServer(t, makeRS256JWKS(t, rsaKey, kid))
	adapter := buildTestAdapter(t, srv.URL, issuer, audience)

	now := time.Now()
	token := signRS256(t, rsaKey, kid, josejwt.Claims{
		Subject:  "user-rs256",
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	}, map[string]any{
		"email":          "rs256user@example.com",
		"email_verified": true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	result := adapter.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("expected authenticated, got reason=%q", result.Reason)
	}
	if result.Identity.ID() != "user-rs256" {
		t.Errorf("ID = %q, want %q", result.Identity.ID(), "user-rs256")
	}
	if result.Identity.Email() != "rs256user@example.com" {
		t.Errorf("Email = %q, want %q", result.Identity.Email(), "rs256user@example.com")
	}
}

func TestIntegration_ES256_ValidToken(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}

	const (
		kid      = "es256-key"
		issuer   = "https://auth.example.com/"
		audience = "integration-test"
	)

	srv, _ := newMockJWKSServer(t, makeES256JWKS(t, ecKey, kid))
	fetcher := jwtadapter.NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, integrationLogger())
	adapter, err := jwtadapter.NewAdapter(jwtadapter.Config{
		JWKSURL:           srv.URL,
		Issuer:            issuer,
		Audience:          audience,
		AllowedAlgorithms: []string{"ES256"},
	}, fetcher, integrationLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	now := time.Now()
	token := signES256(t, ecKey, kid, josejwt.Claims{
		Subject:  "user-es256",
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	result := adapter.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("expected authenticated, got reason=%q", result.Reason)
	}
	if result.Identity.ID() != "user-es256" {
		t.Errorf("ID = %q, want %q", result.Identity.ID(), "user-es256")
	}
}

func TestIntegration_KeyRotation(t *testing.T) {
	rsaKey1, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaKey2, _ := rsa.GenerateKey(rand.Reader, 2048)

	const (
		kid1     = "old-key"
		kid2     = "new-key"
		issuer   = "https://auth.example.com/"
		audience = "integration-test"
	)

	srv, updateJWKS := newMockJWKSServer(t, makeRS256JWKS(t, rsaKey1, kid1))

	fetcher := jwtadapter.NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, integrationLogger())
	adapter, err := jwtadapter.NewAdapter(jwtadapter.Config{
		JWKSURL:  srv.URL,
		Issuer:   issuer,
		Audience: audience,
	}, fetcher, integrationLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	now := time.Now()
	baseClaims := josejwt.Claims{
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	}

	// Token signed with old key should succeed.
	oldClaims := baseClaims
	oldClaims.Subject = "old-user"
	tok1 := signRS256(t, rsaKey1, kid1, oldClaims, nil)
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("Authorization", "Bearer "+tok1)
	r1 := adapter.Authenticate(context.Background(), req1)
	if !r1.Authenticated {
		t.Fatalf("old key: expected authenticated, got %q", r1.Reason)
	}

	// Rotate: update server to serve new key only.
	updateJWKS(makeRS256JWKS(t, rsaKey2, kid2))

	// Token signed with new key should succeed (triggers cache refresh via missing-kid path).
	newClaims := baseClaims
	newClaims.Subject = "new-user"
	tok2 := signRS256(t, rsaKey2, kid2, newClaims, nil)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	r2 := adapter.Authenticate(context.Background(), req2)
	if !r2.Authenticated {
		t.Fatalf("new key: expected authenticated, got %q", r2.Reason)
	}
	if r2.Identity.ID() != "new-user" {
		t.Errorf("ID = %q, want %q", r2.Identity.ID(), "new-user")
	}
}

func TestIntegration_OIDCDiscovery(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	const (
		kid      = "oidc-key"
		issuer   = "https://auth.example.com/"
		audience = "integration-test"
	)

	jwksBody, _ := json.Marshal(makeRS256JWKS(t, rsaKey, kid))

	// Use a pointer so we can capture the server URL inside the handler after creation.
	var srvURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwtadapter.OIDCConfiguration{ //nolint:errcheck
				Issuer:  issuer,
				JwksURI: srvURL + "/jwks",
			})
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			w.Write(jwksBody) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	srvURL = srv.URL
	t.Cleanup(srv.Close)

	// Discover JWKS URL from the mock server's discovery document.
	jwksURL, err := jwtadapter.DiscoverJWKSURL(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("DiscoverJWKSURL: %v", err)
	}

	fetcher := jwtadapter.NewHTTPJWKSFetcher(jwksURL, 5*time.Second, time.Hour, integrationLogger())
	adapter, err := jwtadapter.NewAdapter(jwtadapter.Config{
		JWKSURL:  jwksURL,
		Issuer:   issuer,
		Audience: audience,
	}, fetcher, integrationLogger())
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	now := time.Now()
	token := signRS256(t, rsaKey, kid, josejwt.Claims{
		Subject:  "oidc-user",
		Issuer:   issuer,
		Audience: josejwt.Audience{audience},
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(now),
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	result := adapter.Authenticate(context.Background(), req)
	if !result.Authenticated {
		t.Fatalf("OIDC discovery flow: expected authenticated, got reason=%q", result.Reason)
	}
	if result.Identity.ID() != "oidc-user" {
		t.Errorf("ID = %q, want %q", result.Identity.ID(), "oidc-user")
	}
}
