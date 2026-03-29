package kratos_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/kratos"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Compile-time check: *kratos.Adapter implements ports.IdentityProvider.
var _ ports.IdentityProvider = (*kratos.Adapter)(nil)

// newTestLogger returns a discard logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// validSessionBody returns a minimal Kratos /sessions/whoami JSON payload.
func validSessionBody() map[string]any {
	return map[string]any{
		"id":               "sess-001",
		"active":           true,
		"authenticated_at": "2026-03-24T10:00:00Z",
		"expires_at":       "2026-03-25T10:00:00Z",
		"identity": map[string]any{
			"id": "id-abc-123",
			"traits": map[string]any{
				"email": "user@example.com",
			},
			"verifiable_addresses": []map[string]any{
				{
					"value":    "user@example.com",
					"via":      "email",
					"verified": true,
				},
			},
		},
	}
}

func TestCheckSession_ValidSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validSessionBody())
	}))
	defer srv.Close()

	adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
	session, err := adapter.CheckSession(context.Background(), "ory_kratos_session=abc123")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if session == nil {
		t.Fatal("expected session, got nil")
	}
	if session.ID != "sess-001" {
		t.Errorf("session.ID = %q, want %q", session.ID, "sess-001")
	}
	if !session.Active {
		t.Error("session.Active = false, want true")
	}
	if session.Identity.ID != "id-abc-123" {
		t.Errorf("identity.ID = %q, want %q", session.Identity.ID, "id-abc-123")
	}
	if session.Identity.Email != "user@example.com" {
		t.Errorf("identity.Email = %q, want %q", session.Identity.Email, "user@example.com")
	}
	if !session.Identity.EmailVerified {
		t.Error("identity.EmailVerified = false, want true")
	}
	if session.AuthenticatedAt != "2026-03-24T10:00:00Z" {
		t.Errorf("session.AuthenticatedAt = %q, want %q", session.AuthenticatedAt, "2026-03-24T10:00:00Z")
	}
	if session.ExpiresAt != "2026-03-25T10:00:00Z" {
		t.Errorf("session.ExpiresAt = %q, want %q", session.ExpiresAt, "2026-03-25T10:00:00Z")
	}
}

func TestCheckSession_EmailFallbackFromTraits(t *testing.T) {
	// Kratos response with no verifiable_addresses — email comes from traits.
	body := map[string]any{
		"id":               "sess-002",
		"active":           true,
		"authenticated_at": "2026-03-24T10:00:00Z",
		"expires_at":       "",
		"identity": map[string]any{
			"id": "id-xyz",
			"traits": map[string]any{
				"email": "traits@example.com",
			},
			"verifiable_addresses": []map[string]any{},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
	session, err := adapter.CheckSession(context.Background(), "ory_kratos_session=xyz")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if session.Identity.Email != "traits@example.com" {
		t.Errorf("identity.Email = %q, want %q", session.Identity.Email, "traits@example.com")
	}
	if session.Identity.EmailVerified {
		t.Error("identity.EmailVerified = true, want false (no verifiable address)")
	}
}

func TestCheckSession_InvalidSession(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       any
		wantErr    error
	}{
		{
			name:       "401 from kratos",
			statusCode: http.StatusUnauthorized,
			body:       map[string]any{"error": map[string]any{"message": "session not found"}},
			wantErr:    ports.ErrSessionInvalid,
		},
		{
			name:       "inactive session",
			statusCode: http.StatusOK,
			body: map[string]any{
				"id":     "sess-inactive",
				"active": false,
				"identity": map[string]any{
					"id":     "id-999",
					"traits": map[string]any{},
				},
			},
			wantErr: ports.ErrSessionInvalid,
		},
		{
			name:       "unexpected 4xx status",
			statusCode: http.StatusForbidden,
			body:       map[string]any{"error": "forbidden"},
			wantErr:    ports.ErrSessionInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			}))
			defer srv.Close()

			adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
			_, err := adapter.CheckSession(context.Background(), "ory_kratos_session=bad")

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CheckSession() error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

func TestCheckSession_EmptyCookie(t *testing.T) {
	adapter := kratos.NewAdapter("http://localhost:4433", 0, newTestLogger())
	_, err := adapter.CheckSession(context.Background(), "")

	if !errors.Is(err, ports.ErrSessionNotFound) {
		t.Errorf("CheckSession(\"\") error = %v, want ErrSessionNotFound", err)
	}
}

func TestCheckSession_ProviderUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		setupAddr func() string
	}{
		{
			name: "connection refused",
			setupAddr: func() string {
				// Pick a port that is guaranteed not to be listening.
				return "http://127.0.0.1:1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := kratos.NewAdapter(tt.setupAddr(), 0, newTestLogger())
			_, err := adapter.CheckSession(context.Background(), "ory_kratos_session=abc")

			if !errors.Is(err, ports.ErrAuthProviderUnavailable) {
				t.Errorf("CheckSession() error = %v, want ErrAuthProviderUnavailable", err)
			}
		})
	}
}

func TestCheckSession_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.CheckSession(context.Background(), "ory_kratos_session=abc")

	if !errors.Is(err, ports.ErrAuthProviderUnavailable) {
		t.Errorf("CheckSession() on 500 error = %v, want ErrAuthProviderUnavailable", err)
	}
}

func TestCheckSession_Timeout(t *testing.T) {
	// Server that never responds within the timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Very short timeout to trigger before the handler sleeps.
	adapter := kratos.NewAdapter(srv.URL, 10*time.Millisecond, newTestLogger())
	_, err := adapter.CheckSession(context.Background(), "ory_kratos_session=abc")

	if !errors.Is(err, ports.ErrAuthProviderUnavailable) {
		t.Errorf("CheckSession() timeout error = %v, want ErrAuthProviderUnavailable", err)
	}
}

func TestCheckSession_ContextCancellation(t *testing.T) {
	// Block until the test cancels the context.
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	adapter := kratos.NewAdapter(srv.URL, 5*time.Second, newTestLogger())

	done := make(chan error, 1)
	go func() {
		<-started
		cancel()
	}()
	go func() {
		_, err := adapter.CheckSession(ctx, "ory_kratos_session=abc")
		done <- err
	}()

	err := <-done
	if !errors.Is(err, ports.ErrAuthProviderUnavailable) {
		t.Errorf("CheckSession() context cancelled error = %v, want ErrAuthProviderUnavailable", err)
	}
}

func TestCheckSession_NetworkError(t *testing.T) {
	// Start a server then immediately close it so the connection is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	adapter := kratos.NewAdapter("http://"+addr, 0, newTestLogger())
	_, checkErr := adapter.CheckSession(context.Background(), "ory_kratos_session=abc")

	if !errors.Is(checkErr, ports.ErrAuthProviderUnavailable) {
		t.Errorf("CheckSession() network error = %v, want ErrAuthProviderUnavailable", checkErr)
	}
}

func TestCheckSession_MalformedJSONResponse(t *testing.T) {
	// Kratos returns 200 with a body that is not valid JSON (e.g. proxy injected HTML).
	// The adapter must treat this as ErrAuthProviderUnavailable (fail-closed contract).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>bad gateway</html>`))
	}))
	defer srv.Close()

	adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
	session, err := adapter.CheckSession(context.Background(), "ory_kratos_session=abc")

	if session != nil {
		t.Error("expected nil session on malformed JSON, got non-nil")
	}
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
	if !errors.Is(err, ports.ErrAuthProviderUnavailable) {
		t.Errorf("CheckSession() malformed JSON error = %v, want errors.Is(ErrAuthProviderUnavailable)", err)
	}
}

// ---------------------------------------------------------------------------
// IdentityProvider (Authenticate) tests
// ---------------------------------------------------------------------------

func TestAdapterName(t *testing.T) {
	adapter := kratos.NewAdapter("http://localhost:4433", 0, newTestLogger())
	if got := adapter.Name(); got != "kratos" {
		t.Errorf("Name() = %q, want %q", got, "kratos")
	}
}

func TestAuthenticate_NoCookie(t *testing.T) {
	adapter := kratos.NewAdapter("http://localhost:4433", 0, newTestLogger())
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	result := adapter.Authenticate(context.Background(), req)

	if result.Authenticated {
		t.Error("Authenticate() = authenticated, want not authenticated")
	}
	if result.Reason != "no_credentials" {
		t.Errorf("Reason = %q, want %q", result.Reason, "no_credentials")
	}
}

func TestAuthenticate_ValidSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validSessionBody())
	}))
	defer srv.Close()

	adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "valid-token"})

	result := adapter.Authenticate(context.Background(), req)

	if !result.Authenticated {
		t.Errorf("Authenticate() = not authenticated, want authenticated; reason: %s", result.Reason)
	}
	if result.Identity.IsZero() {
		t.Error("Authenticate() returned zero identity on success")
	}
	if result.Identity.ID() != "id-abc-123" {
		t.Errorf("Identity.ID() = %q, want %q", result.Identity.ID(), "id-abc-123")
	}
	if result.Identity.Email() != "user@example.com" {
		t.Errorf("Identity.Email() = %q, want %q", result.Identity.Email(), "user@example.com")
	}
	if !result.Identity.EmailVerified() {
		t.Error("Identity.EmailVerified() = false, want true")
	}
	if result.Identity.Provider() != "kratos" {
		t.Errorf("Identity.Provider() = %q, want %q", result.Identity.Provider(), "kratos")
	}
}

func TestAuthenticate_InvalidSession(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantReason string
	}{
		{"401 unauthorized", http.StatusUnauthorized, "session_invalid"},
		{"500 server error", http.StatusInternalServerError, "provider_unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			adapter := kratos.NewAdapter(srv.URL, 0, newTestLogger())
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})

			result := adapter.Authenticate(context.Background(), req)

			if result.Authenticated {
				t.Error("Authenticate() = authenticated, want not authenticated")
			}
			if result.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", result.Reason, tt.wantReason)
			}
		})
	}
}

func TestAuthenticate_ProviderUnreachable(t *testing.T) {
	adapter := kratos.NewAdapter("http://127.0.0.1:1", 0, newTestLogger())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "ory_kratos_session", Value: "some-token"})

	result := adapter.Authenticate(context.Background(), req)

	if result.Authenticated {
		t.Error("Authenticate() = authenticated, want not authenticated")
	}
	if result.Reason != "provider_unavailable" {
		t.Errorf("Reason = %q, want %q", result.Reason, "provider_unavailable")
	}
}
