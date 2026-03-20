package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantStatus  string
		wantVersion string
	}{
		{
			name:        "returns ok status and version",
			version:     "v1.0.0",
			wantStatus:  "ok",
			wantVersion: "v1.0.0",
		},
		{
			name:        "returns dev version",
			version:     "dev",
			wantStatus:  "ok",
			wantVersion: "dev",
		},
		{
			name:        "returns empty version",
			version:     "",
			wantStatus:  "ok",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := HealthHandler(tt.version)

			req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
			w := httptest.NewRecorder()

			handler(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var resp HealthResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decoding response: %v", err)
			}

			if resp.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
			}
			if resp.Version != tt.wantVersion {
				t.Errorf("version = %q, want %q", resp.Version, tt.wantVersion)
			}
		})
	}
}

func TestHealthMiddleware_HealthPath(t *testing.T) {
	mw := HealthMiddleware("v0.1.0")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler was called for health path — should not be")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}

func TestHealthMiddleware_OtherPath(t *testing.T) {
	mw := HealthMiddleware("v0.1.0")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(next)

	paths := []string{"/", "/api/users", "/health", "/_vibewarden/other"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			nextCalled = false

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("next handler was not called for path %q", path)
			}
		})
	}
}
