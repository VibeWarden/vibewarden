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
