package jwt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscoverJWKSURL(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantJWKSURL string
		wantErr     bool
	}{
		{
			name: "valid OIDC discovery document",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(OIDCConfiguration{ //nolint:errcheck
					Issuer:  "https://auth.example.com",
					JwksURI: "https://auth.example.com/.well-known/jwks.json",
				})
			},
			wantJWKSURL: "https://auth.example.com/.well-known/jwks.json",
			wantErr:     false,
		},
		{
			name: "discovery URL normalises trailing slash on issuer",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(OIDCConfiguration{ //nolint:errcheck
					Issuer:  "https://auth.example.com/",
					JwksURI: "https://auth.example.com/jwks",
				})
			},
			wantJWKSURL: "https://auth.example.com/jwks",
			wantErr:     false,
		},
		{
			name: "server returns non-200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			name: "server returns invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("not-json")) //nolint:errcheck
			},
			wantErr: true,
		},
		{
			name: "jwks_uri missing from response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// Return a document without jwks_uri.
				json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
					"issuer": "https://auth.example.com/",
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			// Build the issuer URL that points to the test server.
			issuerURL := srv.URL
			if tt.name == "discovery URL normalises trailing slash on issuer" {
				issuerURL = srv.URL + "/"
			}

			jwksURL, err := DiscoverJWKSURL(context.Background(), issuerURL, 5*time.Second)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverJWKSURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && jwksURL != tt.wantJWKSURL {
				t.Errorf("DiscoverJWKSURL() = %q, want %q", jwksURL, tt.wantJWKSURL)
			}
		})
	}
}

func TestDiscoverJWKSURL_DefaultTimeout(t *testing.T) {
	// Server that hangs to test timeout behaviour.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not respond; let the timeout fire.
		<-r.Context().Done()
	}))
	defer srv.Close()

	// Use a very short timeout to avoid slowing down the test suite.
	_, err := DiscoverJWKSURL(context.Background(), srv.URL, 50*time.Millisecond)
	if err == nil {
		t.Error("DiscoverJWKSURL: expected timeout error, got nil")
	}
}

func TestDiscoverJWKSURL_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := DiscoverJWKSURL(ctx, srv.URL, 5*time.Second)
	if err == nil {
		t.Error("DiscoverJWKSURL: expected error from cancelled context, got nil")
	}
}
