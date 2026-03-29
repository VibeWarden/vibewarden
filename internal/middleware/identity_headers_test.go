package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/identity"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestIdentityHeadersMiddleware_WithIdentity(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		email         string
		emailVerified bool
		wantID        string
		wantEmail     string
		wantVerified  string
	}{
		{
			name:          "verified user",
			id:            "user-abc",
			email:         "alice@example.com",
			emailVerified: true,
			wantID:        "user-abc",
			wantEmail:     "alice@example.com",
			wantVerified:  "true",
		},
		{
			name:          "unverified user",
			id:            "user-def",
			email:         "bob@example.com",
			emailVerified: false,
			wantID:        "user-def",
			wantEmail:     "bob@example.com",
			wantVerified:  "false",
		},
		{
			name:         "no email",
			id:           "user-ghi",
			email:        "",
			wantID:       "user-ghi",
			wantEmail:    "",
			wantVerified: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHeaders http.Header
			mw := IdentityHeadersMiddleware(newTestLogger())
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeaders = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
			})

			ident, err := identity.NewIdentity(tt.id, tt.email, "kratos", tt.emailVerified, nil)
			if err != nil {
				t.Fatalf("NewIdentity() error: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/app", nil)
			req = req.WithContext(contextWithIdentity(req.Context(), ident))

			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if got := gotHeaders.Get("X-User-Id"); got != tt.wantID {
				t.Errorf("X-User-Id = %q, want %q", got, tt.wantID)
			}
			if got := gotHeaders.Get("X-User-Email"); got != tt.wantEmail {
				t.Errorf("X-User-Email = %q, want %q", got, tt.wantEmail)
			}
			if got := gotHeaders.Get("X-User-Verified"); got != tt.wantVerified {
				t.Errorf("X-User-Verified = %q, want %q", got, tt.wantVerified)
			}
		})
	}
}

// TestIdentityHeadersMiddleware_WithSession verifies backward compatibility:
// the middleware falls back to reading from the deprecated ports.Session when
// no domain Identity is present in the context.
func TestIdentityHeadersMiddleware_WithSession(t *testing.T) {
	tests := []struct {
		name         string
		session      *ports.Session
		wantID       string
		wantEmail    string
		wantVerified string
	}{
		{
			name: "verified user (legacy session)",
			session: &ports.Session{
				ID:     "sess-1",
				Active: true,
				Identity: ports.Identity{
					ID:            "user-abc",
					Email:         "alice@example.com",
					EmailVerified: true,
				},
			},
			wantID:       "user-abc",
			wantEmail:    "alice@example.com",
			wantVerified: "true",
		},
		{
			name: "unverified user (legacy session)",
			session: &ports.Session{
				ID:     "sess-2",
				Active: true,
				Identity: ports.Identity{
					ID:            "user-def",
					Email:         "bob@example.com",
					EmailVerified: false,
				},
			},
			wantID:       "user-def",
			wantEmail:    "bob@example.com",
			wantVerified: "false",
		},
		{
			name: "no email (legacy session)",
			session: &ports.Session{
				ID:     "sess-3",
				Active: true,
				Identity: ports.Identity{
					ID:            "user-ghi",
					Email:         "",
					EmailVerified: false,
				},
			},
			wantID:       "user-ghi",
			wantEmail:    "",
			wantVerified: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHeaders http.Header
			mw := IdentityHeadersMiddleware(newTestLogger())
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeaders = r.Header.Clone()
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/app", nil)
			// Store the session in the context using the deprecated path.
			req = req.WithContext(contextWithSession(req.Context(), tt.session))

			w := httptest.NewRecorder()
			mw(next).ServeHTTP(w, req)

			if got := gotHeaders.Get("X-User-Id"); got != tt.wantID {
				t.Errorf("X-User-Id = %q, want %q", got, tt.wantID)
			}
			if got := gotHeaders.Get("X-User-Email"); got != tt.wantEmail {
				t.Errorf("X-User-Email = %q, want %q", got, tt.wantEmail)
			}
			if got := gotHeaders.Get("X-User-Verified"); got != tt.wantVerified {
				t.Errorf("X-User-Verified = %q, want %q", got, tt.wantVerified)
			}
		})
	}
}

func TestIdentityHeadersMiddleware_NoSession(t *testing.T) {
	var gotHeaders http.Header
	nextCalled := false

	mw := IdentityHeadersMiddleware(newTestLogger())
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	// No session or identity in context (public path scenario).
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Fatal("next handler was not called")
	}

	for _, h := range []string{"X-User-Id", "X-User-Email", "X-User-Verified"} {
		if gotHeaders.Get(h) != "" {
			t.Errorf("header %q should not be set when no identity in context, got %q", h, gotHeaders.Get(h))
		}
	}
}

func TestIdentityHeadersMiddleware_NextAlwaysCalled(t *testing.T) {
	nextCalled := false
	mw := IdentityHeadersMiddleware(newTestLogger())
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler was not called")
	}
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}
