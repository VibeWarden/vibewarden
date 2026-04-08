package ops_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

func TestHTTPHealthChecker_CheckHealth(t *testing.T) {
	tests := []struct {
		name           string
		handlerStatus  int
		wantOK         bool
		wantStatusCode int
	}{
		{"200 OK", http.StatusOK, true, 200},
		{"204 No Content", http.StatusNoContent, true, 204},
		{"400 Bad Request", http.StatusBadRequest, false, 400},
		{"503 Service Unavailable", http.StatusServiceUnavailable, false, 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.handlerStatus)
			}))
			defer srv.Close()

			checker := opsadapter.NewHTTPHealthChecker(srv.Client())
			ok, code, err := checker.CheckHealth(context.Background(), srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if code != tt.wantStatusCode {
				t.Errorf("statusCode = %d, want %d", code, tt.wantStatusCode)
			}
		})
	}
}

func TestHTTPHealthChecker_CheckHealth_Unreachable(t *testing.T) {
	checker := opsadapter.NewHTTPHealthChecker(http.DefaultClient)
	ok, _, err := checker.CheckHealth(context.Background(), "http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
	if ok {
		t.Error("expected ok = false for unreachable server")
	}
}

func TestHTTPHealthChecker_CheckHealth_SelfSignedTLS(t *testing.T) {
	// httptest.NewTLSServer uses a self-signed certificate. The client returned
	// by srv.Client() is pre-configured to trust it, simulating the
	// InsecureSkipVerify path used by newStatusHTTPClient when provider is
	// "self-signed".
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// srv.Client() trusts the test CA — this is what our production code
	// achieves by setting InsecureSkipVerify for the self-signed provider.
	checker := opsadapter.NewHTTPHealthChecker(srv.Client())
	ok, code, err := checker.CheckHealth(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error against TLS server: %v", err)
	}
	if !ok {
		t.Errorf("ok = false, want true")
	}
	if code != http.StatusOK {
		t.Errorf("statusCode = %d, want %d", code, http.StatusOK)
	}
}

func TestHTTPHealthChecker_CheckHealth_SelfSignedTLS_DefaultClientFails(t *testing.T) {
	// Verify that a client without InsecureSkipVerify CANNOT reach the TLS server —
	// this confirms the fix is actually needed.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := opsadapter.NewHTTPHealthChecker(http.DefaultClient)
	ok, _, err := checker.CheckHealth(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected TLS verification error with default client, got nil")
	}
	if ok {
		t.Error("expected ok = false when TLS verification fails")
	}
}
