package kratos_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/kratos"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// identityFixture returns a minimal Kratos admin identity JSON payload.
func identityFixture(id, email, state string) map[string]any {
	return map[string]any{
		"id":         id,
		"state":      state,
		"created_at": "2026-03-20T10:00:00Z",
		"traits": map[string]any{
			"email": email,
		},
	}
}

func TestAdminAdapter_ListUsers_Success(t *testing.T) {
	fixtures := []map[string]any{
		identityFixture("id-001", "alice@example.com", "active"),
		identityFixture("id-002", "bob@example.com", "inactive"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/identities" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixtures)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	result, err := adapter.ListUsers(context.Background(), ports.Pagination{Page: 1, PerPage: 10})

	if err != nil {
		t.Fatalf("ListUsers() error = %v, want nil", err)
	}
	if len(result.Users) != 2 {
		t.Fatalf("len(Users) = %d, want 2", len(result.Users))
	}
	if result.Users[0].Email != "alice@example.com" {
		t.Errorf("Users[0].Email = %q, want %q", result.Users[0].Email, "alice@example.com")
	}
	if result.Users[0].Status != user.StatusActive {
		t.Errorf("Users[0].Status = %q, want %q", result.Users[0].Status, user.StatusActive)
	}
	if result.Users[1].Status != user.StatusInactive {
		t.Errorf("Users[1].Status = %q, want %q", result.Users[1].Status, user.StatusInactive)
	}
}

func TestAdminAdapter_ListUsers_DefaultPagination(t *testing.T) {
	// Verify that zero-value pagination is normalised to page=1 per_page=25.
	var gotPage, gotPerPage string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPage = r.URL.Query().Get("page")
		gotPerPage = r.URL.Query().Get("per_page")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.ListUsers(context.Background(), ports.Pagination{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if gotPage != "1" {
		t.Errorf("page = %q, want %q", gotPage, "1")
	}
	if gotPerPage != "25" {
		t.Errorf("per_page = %q, want %q", gotPerPage, "25")
	}
}

func TestAdminAdapter_ListUsers_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.ListUsers(context.Background(), ports.Pagination{Page: 1, PerPage: 10})

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("ListUsers() on 500 error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_ListUsers_NetworkError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	adapter := kratos.NewAdminAdapter("http://"+addr, 0, newTestLogger())
	_, err = adapter.ListUsers(context.Background(), ports.Pagination{Page: 1, PerPage: 10})

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("ListUsers() network error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_GetUser_Success(t *testing.T) {
	const identityID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/identities/"+identityID {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identityFixture(identityID, "carol@example.com", "active"))
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	u, err := adapter.GetUser(context.Background(), identityID)

	if err != nil {
		t.Fatalf("GetUser() error = %v, want nil", err)
	}
	if u.ID != identityID {
		t.Errorf("ID = %q, want %q", u.ID, identityID)
	}
	if u.Email != "carol@example.com" {
		t.Errorf("Email = %q, want %q", u.Email, "carol@example.com")
	}
	if u.Status != user.StatusActive {
		t.Errorf("Status = %q, want %q", u.Status, user.StatusActive)
	}
}

func TestAdminAdapter_GetUser_NotFound(t *testing.T) {
	const identityID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.GetUser(context.Background(), identityID)

	if !errors.Is(err, ports.ErrUserNotFound) {
		t.Errorf("GetUser() on 404 error = %v, want ErrUserNotFound", err)
	}
}

func TestAdminAdapter_GetUser_InvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"not-a-uuid", "not-a-uuid"},
		{"too-short", "abc123"},
		{"uppercase-uuid-still-valid", "A1B2C3D4-E5F6-7890-ABCD-EF1234567890"},
	}

	adapter := kratos.NewAdminAdapter("http://localhost:4434", 0, newTestLogger())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Uppercase UUIDs should be accepted (normalised internally).
			if tt.name == "uppercase-uuid-still-valid" {
				_, err := adapter.GetUser(context.Background(), tt.id)
				// This will fail at network level, not UUID validation.
				// Verify it does NOT return ErrInvalidUUID.
				if errors.Is(err, ports.ErrInvalidUUID) {
					t.Errorf("GetUser(%q) returned ErrInvalidUUID, uppercase UUIDs should be accepted", tt.id)
				}
				return
			}
			_, err := adapter.GetUser(context.Background(), tt.id)
			if !errors.Is(err, ports.ErrInvalidUUID) {
				t.Errorf("GetUser(%q) error = %v, want ErrInvalidUUID", tt.id, err)
			}
		})
	}
}

func TestAdminAdapter_GetUser_ServerError(t *testing.T) {
	const identityID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.GetUser(context.Background(), identityID)

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("GetUser() on 500 error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_InviteUser_Success(t *testing.T) {
	const newID = "b1b2c3d4-e5f6-7890-abcd-ef1234567891"
	const wantLink = "https://kratos.example.com/self-service/recovery?token=abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/identities":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(identityFixture(newID, "new@example.com", "active"))

		case r.Method == http.MethodPost && r.URL.Path == "/admin/recovery/link":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"recovery_link": wantLink})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	result, err := adapter.InviteUser(context.Background(), "new@example.com")

	if err != nil {
		t.Fatalf("InviteUser() error = %v, want nil", err)
	}
	if result.User.ID != newID {
		t.Errorf("User.ID = %q, want %q", result.User.ID, newID)
	}
	if result.User.Email != "new@example.com" {
		t.Errorf("User.Email = %q, want %q", result.User.Email, "new@example.com")
	}
	if result.RecoveryLink != wantLink {
		t.Errorf("RecoveryLink = %q, want %q", result.RecoveryLink, wantLink)
	}
}

func TestAdminAdapter_InviteUser_RecoveryLinkFailureIsNonFatal(t *testing.T) {
	// The recovery-link endpoint fails — InviteUser should still succeed,
	// returning the user with an empty RecoveryLink.
	const newID = "b1b2c3d4-e5f6-7890-abcd-ef1234567892"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/identities":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(identityFixture(newID, "fragile@example.com", "active"))

		case r.Method == http.MethodPost && r.URL.Path == "/admin/recovery/link":
			w.WriteHeader(http.StatusInternalServerError)

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	result, err := adapter.InviteUser(context.Background(), "fragile@example.com")

	if err != nil {
		t.Fatalf("InviteUser() error = %v, want nil (recovery link failure should be non-fatal)", err)
	}
	if result.RecoveryLink != "" {
		t.Errorf("RecoveryLink = %q, want empty string when recovery fails", result.RecoveryLink)
	}
}

func TestAdminAdapter_InviteUser_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.InviteUser(context.Background(), "existing@example.com")

	if !errors.Is(err, ports.ErrUserAlreadyExists) {
		t.Errorf("InviteUser() on 409 error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestAdminAdapter_InviteUser_InvalidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
	}{
		{"empty", ""},
		{"no-at", "notanemail"},
		{"no-domain", "user@"},
		{"no-local", "@example.com"},
		{"whitespace", "user @example.com"},
	}

	adapter := kratos.NewAdminAdapter("http://localhost:4434", 0, newTestLogger())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adapter.InviteUser(context.Background(), tt.email)
			if !errors.Is(err, ports.ErrInvalidEmail) {
				t.Errorf("InviteUser(%q) error = %v, want ErrInvalidEmail", tt.email, err)
			}
		})
	}
}

func TestAdminAdapter_InviteUser_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	_, err := adapter.InviteUser(context.Background(), "ok@example.com")

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("InviteUser() on 500 error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_DeactivateUser_Success(t *testing.T) {
	const identityID = "c1b2c3d4-e5f6-7890-abcd-ef1234567890"

	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(identityFixture(identityID, "dave@example.com", "inactive"))
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	err := adapter.DeactivateUser(context.Background(), identityID)

	if err != nil {
		t.Fatalf("DeactivateUser() error = %v, want nil", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/admin/identities/"+identityID {
		t.Errorf("path = %q, want /admin/identities/%s", gotPath, identityID)
	}
	if gotBody["state"] != "inactive" {
		t.Errorf("body.state = %v, want \"inactive\"", gotBody["state"])
	}
}

func TestAdminAdapter_DeactivateUser_NotFound(t *testing.T) {
	const identityID = "d1b2c3d4-e5f6-7890-abcd-ef1234567890"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	err := adapter.DeactivateUser(context.Background(), identityID)

	if !errors.Is(err, ports.ErrUserNotFound) {
		t.Errorf("DeactivateUser() on 404 error = %v, want ErrUserNotFound", err)
	}
}

func TestAdminAdapter_DeactivateUser_InvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"plain-string", "user-1234"},
		{"partial-uuid", "a1b2c3d4-e5f6"},
	}

	adapter := kratos.NewAdminAdapter("http://localhost:4434", 0, newTestLogger())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.DeactivateUser(context.Background(), tt.id)
			if !errors.Is(err, ports.ErrInvalidUUID) {
				t.Errorf("DeactivateUser(%q) error = %v, want ErrInvalidUUID", tt.id, err)
			}
		})
	}
}

func TestAdminAdapter_DeactivateUser_ServerError(t *testing.T) {
	const identityID = "e1b2c3d4-e5f6-7890-abcd-ef1234567890"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := kratos.NewAdminAdapter(srv.URL, 0, newTestLogger())
	err := adapter.DeactivateUser(context.Background(), identityID)

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("DeactivateUser() on 500 error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_DeactivateUser_NetworkError(t *testing.T) {
	const identityID = "f1b2c3d4-e5f6-7890-abcd-ef1234567890"

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	adapter := kratos.NewAdminAdapter("http://"+addr, 0, newTestLogger())
	err = adapter.DeactivateUser(context.Background(), identityID)

	if !errors.Is(err, ports.ErrAdminUnavailable) {
		t.Errorf("DeactivateUser() network error = %v, want ErrAdminUnavailable", err)
	}
}

func TestAdminAdapter_ImplementsUserAdminInterface(t *testing.T) {
	// Compile-time assertion: AdminAdapter must satisfy ports.UserAdmin.
	var _ ports.UserAdmin = (*kratos.AdminAdapter)(nil)
}
