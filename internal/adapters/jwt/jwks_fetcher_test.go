package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// buildTestJWKS builds a JSON Web Key Set with the given RSA public key.
func buildTestJWKS(t *testing.T, key *rsa.PrivateKey, kid string) []byte {
	t.Helper()
	jwks := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{
				Key:       &key.PublicKey,
				KeyID:     kid,
				Algorithm: "RS256",
				Use:       "sig",
			},
		},
	}
	data, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return data
}

func TestHTTPJWKSFetcher_FetchKeys(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksBody := buildTestJWKS(t, rsaKey, "key1")

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody) //nolint:errcheck
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())

	// First call should fetch from server.
	jwks, err := fetcher.FetchKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchKeys: %v", err)
	}
	if len(jwks.Keys) != 1 {
		t.Errorf("FetchKeys: got %d keys, want 1", len(jwks.Keys))
	}
	if fetchCount != 1 {
		t.Errorf("expected 1 server request, got %d", fetchCount)
	}

	// Second call should return cached result without hitting server.
	jwks2, err := fetcher.FetchKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchKeys (cached): %v", err)
	}
	if len(jwks2.Keys) != 1 {
		t.Errorf("FetchKeys (cached): got %d keys, want 1", len(jwks2.Keys))
	}
	if fetchCount != 1 {
		t.Errorf("expected still 1 server request, got %d", fetchCount)
	}
}

func TestHTTPJWKSFetcher_FetchKeys_RefreshOnTTLExpiry(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksBody := buildTestJWKS(t, rsaKey, "key1")
	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody) //nolint:errcheck
	}))
	defer srv.Close()

	// Very short TTL to force expiry.
	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Millisecond, testLogger())

	// First call.
	if _, err := fetcher.FetchKeys(context.Background()); err != nil {
		t.Fatalf("FetchKeys: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// Should re-fetch.
	if _, err := fetcher.FetchKeys(context.Background()); err != nil {
		t.Fatalf("FetchKeys after expiry: %v", err)
	}
	if fetchCount < 2 {
		t.Errorf("expected at least 2 server requests, got %d", fetchCount)
	}
}

func TestHTTPJWKSFetcher_GetKey_Found(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksBody := buildTestJWKS(t, rsaKey, "key1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody) //nolint:errcheck
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())

	key, err := fetcher.GetKey(context.Background(), "key1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if key.KeyID != "key1" {
		t.Errorf("GetKey: KeyID = %q, want %q", key.KeyID, "key1")
	}
}

func TestHTTPJWKSFetcher_GetKey_TriggersRefreshOnMissingKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	// First response has key1; after first fetch, key2 appears (key rotation).
	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		kid := "key1"
		if fetchCount > 1 {
			kid = "key2"
		}
		body := buildTestJWKS(t, rsaKey, kid)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}))
	defer srv.Close()

	// Populate cache with key1 first.
	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())
	if _, err := fetcher.FetchKeys(context.Background()); err != nil {
		t.Fatalf("FetchKeys: %v", err)
	}
	if fetchCount != 1 {
		t.Fatalf("expected 1 initial fetch, got %d", fetchCount)
	}

	// Request key2 which does not exist in cache; should trigger refresh.
	key, err := fetcher.GetKey(context.Background(), "key2")
	if err != nil {
		t.Fatalf("GetKey(key2): %v", err)
	}
	if key.KeyID != "key2" {
		t.Errorf("GetKey: KeyID = %q, want %q", key.KeyID, "key2")
	}
	if fetchCount < 2 {
		t.Errorf("expected at least 2 fetches after key rotation, got %d", fetchCount)
	}
}

func TestHTTPJWKSFetcher_GetKey_NotFound(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksBody := buildTestJWKS(t, rsaKey, "key1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody) //nolint:errcheck
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())
	_, err = fetcher.GetKey(context.Background(), "nonexistent-kid")
	if err == nil {
		t.Fatal("GetKey(nonexistent): expected error, got nil")
	}
}

func TestHTTPJWKSFetcher_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())
	_, err := fetcher.FetchKeys(context.Background())
	if err == nil {
		t.Fatal("FetchKeys: expected error for 500 response, got nil")
	}
}

func TestHTTPJWKSFetcher_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json")) //nolint:errcheck
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())
	_, err := fetcher.FetchKeys(context.Background())
	if err == nil {
		t.Fatal("FetchKeys: expected error for invalid JSON, got nil")
	}
}

func TestHTTPJWKSFetcher_ConcurrentAccess(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksBody := buildTestJWKS(t, rsaKey, "key1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond) // Simulate network latency.
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody) //nolint:errcheck
	}))
	defer srv.Close()

	fetcher := NewHTTPJWKSFetcher(srv.URL, 5*time.Second, time.Hour, testLogger())

	// Launch multiple goroutines to fetch concurrently.
	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = fetcher.FetchKeys(context.Background())
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: FetchKeys error: %v", i, e)
		}
	}
}

func TestHTTPJWKSFetcher_DefaultTimeout(t *testing.T) {
	f := NewHTTPJWKSFetcher("https://example.com/jwks", 0, 0, testLogger())
	if f.client.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", f.client.Timeout)
	}
	if f.cacheTTL != time.Hour {
		t.Errorf("default cacheTTL = %v, want 1h", f.cacheTTL)
	}
}
