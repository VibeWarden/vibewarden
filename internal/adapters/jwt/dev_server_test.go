package jwt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestDevServer_StartStop(t *testing.T) {
	dir := t.TempDir()
	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	srv := NewDevServer(kp, testLogger())
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	if srv.Addr() == "" {
		t.Fatal("Addr() returned empty string after Start")
	}
	if srv.LocalJWKSURL() == "" {
		t.Fatal("LocalJWKSURL() returned empty string after Start")
	}
}

func TestDevServer_ServesValidJWKS(t *testing.T) {
	dir := t.TempDir()
	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	srv := NewDevServer(kp, testLogger())
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	resp, err := http.Get(srv.LocalJWKSURL()) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", srv.LocalJWKSURL(), err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		t.Fatalf("parsing JWKS: %v", err)
	}
	if len(jwks.Keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(jwks.Keys))
	}
	if jwks.Keys[0].KeyID != DevKID {
		t.Errorf("kid = %q, want %q", jwks.Keys[0].KeyID, DevKID)
	}
}

func TestDevServer_LocalJWKSURL_Format(t *testing.T) {
	dir := t.TempDir()
	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	srv := NewDevServer(kp, testLogger())
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	jwksURL := srv.LocalJWKSURL()
	want := "http://" + srv.Addr() + DevJWKSPath
	if jwksURL != want {
		t.Errorf("LocalJWKSURL() = %q, want %q", jwksURL, want)
	}
}

func TestDevServer_ServesTokenEndpoint(t *testing.T) {
	dir := t.TempDir()
	kp, err := LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerateDevKeys: %v", err)
	}

	srv := NewDevServer(kp, testLogger())
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	tokenURL := "http://" + srv.Addr() + DevTokenPath
	resp, err := http.Get(tokenURL) //nolint:noctx,gosec // test-only: internal localhost URL
	if err != nil {
		t.Fatalf("GET %s: %v", tokenURL, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	if body.Token == "" {
		t.Error("token response has empty token field")
	}
}
