package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
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

	mw := KratosFlowLoggingMiddleware(logger, nil)(next)

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

	mw := KratosFlowLoggingMiddleware(logger, nil)(next)

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

func TestKratosFlowLoggingMiddleware_EmitsEventForKratosPath(t *testing.T) {
	spy := &fakeEventLogger{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := KratosFlowLoggingMiddleware(slog.Default(), spy)(next)

	req := httptest.NewRequest(http.MethodGet, "/self-service/login/browser", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !spy.hasEventType(events.EventTypeProxyKratosFlow) {
		t.Error("expected proxy.kratos_flow event but none was logged")
	}
	if len(spy.logged) == 0 {
		t.Fatal("no events logged")
	}
	ev := spy.logged[0]
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Payload["path"] != "/self-service/login/browser" {
		t.Errorf("payload.path = %v, want %q", ev.Payload["path"], "/self-service/login/browser")
	}
}

func TestKratosFlowLoggingMiddleware_DoesNotEmitEventForNonKratosPath(t *testing.T) {
	spy := &fakeEventLogger{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := KratosFlowLoggingMiddleware(slog.Default(), spy)(next)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(spy.logged) != 0 {
		t.Errorf("expected no events for non-Kratos path, got: %v", spy.logged)
	}
}

func TestKratosFlowLoggingMiddleware_NilEventLoggerDoesNotPanic(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Must not panic with nil eventLogger.
	mw := KratosFlowLoggingMiddleware(slog.Default(), nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/self-service/login/browser", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("response code = %d, want %d", rec.Code, http.StatusOK)
	}
}
