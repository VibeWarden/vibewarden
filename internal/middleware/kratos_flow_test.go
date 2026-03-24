package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsKratosFlowPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "self-service login browser",
			path: "/self-service/login/browser",
			want: true,
		},
		{
			name: "self-service registration",
			path: "/self-service/registration/browser",
			want: true,
		},
		{
			name: "self-service logout",
			path: "/self-service/logout",
			want: true,
		},
		{
			name: "self-service settings",
			path: "/self-service/settings/browser",
			want: true,
		},
		{
			name: "self-service recovery",
			path: "/self-service/recovery/browser",
			want: true,
		},
		{
			name: "self-service verification",
			path: "/self-service/verification/browser",
			want: true,
		},
		{
			name: "ory kratos public prefix",
			path: "/.ory/kratos/public/sessions/whoami",
			want: true,
		},
		{
			name: "ory kratos public root",
			path: "/.ory/kratos/public/",
			want: true,
		},
		{
			name: "vibewarden health check",
			path: "/_vibewarden/health",
			want: false,
		},
		{
			name: "root path",
			path: "/",
			want: false,
		},
		{
			name: "app route",
			path: "/dashboard",
			want: false,
		},
		{
			name: "api route",
			path: "/api/v1/users",
			want: false,
		},
		{
			name: "partial match prefix not a flow",
			path: "/self-servicex/login",
			want: false,
		},
		{
			name: "empty path",
			path: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKratosFlowPath(tt.path)
			if got != tt.want {
				t.Errorf("isKratosFlowPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestKratosFlowLoggingMiddleware_CallsNext(t *testing.T) {
	logger := slog.Default()
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := KratosFlowLoggingMiddleware(logger)(next)

	req := httptest.NewRequest(http.MethodGet, "/self-service/login/browser", nil)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("KratosFlowLoggingMiddleware must call the next handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("response code = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestKratosFlowLoggingMiddleware_NonKratosPathCallsNext(t *testing.T) {
	logger := slog.Default()
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	mw := KratosFlowLoggingMiddleware(logger)(next)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("KratosFlowLoggingMiddleware must call next even for non-Kratos paths")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("response code = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
