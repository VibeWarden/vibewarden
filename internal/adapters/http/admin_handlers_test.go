package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vibehttp "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeAdminService implements ports.AdminService for tests.
type fakeAdminService struct {
	listResult *ports.PaginatedUsers
	listErr    error

	getResult *user.User
	getErr    error

	inviteResult *ports.InviteResult
	inviteErr    error

	deactivateErr error

	// Recorded calls.
	lastListPagination  ports.Pagination
	lastGetID           string
	lastInviteEmail     string
	lastInviteActor     string
	lastDeactivateID    string
	lastDeactivateActor string
}

func (f *fakeAdminService) ListUsers(_ context.Context, p ports.Pagination) (*ports.PaginatedUsers, error) {
	f.lastListPagination = p
	return f.listResult, f.listErr
}

func (f *fakeAdminService) GetUser(_ context.Context, id string) (*user.User, error) {
	f.lastGetID = id
	return f.getResult, f.getErr
}

func (f *fakeAdminService) InviteUser(_ context.Context, email string, actorID string) (*ports.InviteResult, error) {
	f.lastInviteEmail = email
	f.lastInviteActor = actorID
	return f.inviteResult, f.inviteErr
}

func (f *fakeAdminService) DeactivateUser(_ context.Context, userID string, actorID string, _ string) error {
	f.lastDeactivateID = userID
	f.lastDeactivateActor = actorID
	return f.deactivateErr
}

func makeTestUser(id, email string, status user.Status) *user.User {
	return &user.User{
		ID:        id,
		Email:     email,
		Status:    status,
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func newMux(svc ports.AdminService) *http.ServeMux {
	mux := http.NewServeMux()
	vibehttp.NewAdminHandlers(svc, nil).RegisterRoutes(mux)
	return mux
}

// ------------------------------------------------------------------
// GET /_vibewarden/admin/users
// ------------------------------------------------------------------

func TestListUsers_Success(t *testing.T) {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{
			Users: []user.User{
				*makeTestUser("id-1", "alice@example.com", user.StatusActive),
				*makeTestUser("id-2", "bob@example.com", user.StatusInactive),
			},
			Total: 2,
		},
	}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users?page=2&per_page=10", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	users, ok := resp["users"].([]any)
	if !ok {
		t.Fatal("response missing users array")
	}
	if len(users) != 2 {
		t.Errorf("users count = %d, want 2", len(users))
	}

	// Verify pagination was passed through.
	if svc.lastListPagination.Page != 2 {
		t.Errorf("page = %d, want 2", svc.lastListPagination.Page)
	}
	if svc.lastListPagination.PerPage != 10 {
		t.Errorf("per_page = %d, want 10", svc.lastListPagination.PerPage)
	}
}

func TestListUsers_DefaultPagination(t *testing.T) {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{Users: []user.User{}, Total: 0},
	}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if svc.lastListPagination.Page != 1 {
		t.Errorf("default page = %d, want 1", svc.lastListPagination.Page)
	}
	if svc.lastListPagination.PerPage != 20 {
		t.Errorf("default per_page = %d, want 20", svc.lastListPagination.PerPage)
	}
}

func TestListUsers_PerPageCappedAt100(t *testing.T) {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{Users: []user.User{}, Total: 0},
	}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users?per_page=9999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if svc.lastListPagination.PerPage != 100 {
		t.Errorf("capped per_page = %d, want 100", svc.lastListPagination.PerPage)
	}
}

func TestListUsers_ServiceUnavailable(t *testing.T) {
	svc := &fakeAdminService{listErr: ports.ErrAdminUnavailable}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service_unavailable")
}

// ------------------------------------------------------------------
// GET /_vibewarden/admin/users/{id}
// ------------------------------------------------------------------

func TestGetUser_Success(t *testing.T) {
	svc := &fakeAdminService{
		getResult: makeTestUser("550e8400-e29b-41d4-a716-446655440000", "alice@example.com", user.StatusActive),
	}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", resp["email"])
	}
	if resp["status"] != "active" {
		t.Errorf("status = %v, want active", resp["status"])
	}
}

func TestGetUser_InvalidUUID(t *testing.T) {
	svc := &fakeAdminService{}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid_uuid")
}

func TestGetUser_NotFound(t *testing.T) {
	svc := &fakeAdminService{getErr: ports.ErrUserNotFound}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusNotFound, "user_not_found")
}

func TestGetUser_ServiceUnavailable(t *testing.T) {
	svc := &fakeAdminService{getErr: ports.ErrAdminUnavailable}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service_unavailable")
}

// ------------------------------------------------------------------
// POST /_vibewarden/admin/users
// ------------------------------------------------------------------

func TestInviteUser_Success(t *testing.T) {
	svc := &fakeAdminService{
		inviteResult: &ports.InviteResult{
			User:         *makeTestUser("id-new", "new@example.com", user.StatusActive),
			RecoveryLink: "https://kratos.example.com/recovery?token=abc",
		},
	}
	mux := newMux(svc)

	body := `{"email":"new@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["email"] != "new@example.com" {
		t.Errorf("email = %v, want new@example.com", resp["email"])
	}
	if resp["recovery_link"] != "https://kratos.example.com/recovery?token=abc" {
		t.Errorf("recovery_link = %v", resp["recovery_link"])
	}
}

func TestInviteUser_InvalidEmail(t *testing.T) {
	svc := &fakeAdminService{}
	mux := newMux(svc)

	body := `{"email":"not-an-email"}`
	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid_email")
}

func TestInviteUser_MalformedJSON(t *testing.T) {
	svc := &fakeAdminService{}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/users", bytes.NewBufferString(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid_request")
}

func TestInviteUser_Conflict(t *testing.T) {
	svc := &fakeAdminService{inviteErr: ports.ErrUserAlreadyExists}
	mux := newMux(svc)

	body := `{"email":"existing@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusConflict, "user_exists")
}

func TestInviteUser_ServiceUnavailable(t *testing.T) {
	svc := &fakeAdminService{inviteErr: ports.ErrAdminUnavailable}
	mux := newMux(svc)

	body := `{"email":"new@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service_unavailable")
}

// ------------------------------------------------------------------
// DELETE /_vibewarden/admin/users/{id}
// ------------------------------------------------------------------

func TestDeactivateUser_Success(t *testing.T) {
	// DeactivateUser calls GetUser first, then Deactivate.
	// The fake service handles both through DeactivateUser.
	svc := &fakeAdminService{}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "deactivated" {
		t.Errorf("status = %v, want deactivated", resp["status"])
	}
	if svc.lastDeactivateID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("deactivateID = %q, want UUID", svc.lastDeactivateID)
	}
}

func TestDeactivateUser_InvalidUUID(t *testing.T) {
	svc := &fakeAdminService{}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid_uuid")
}

func TestDeactivateUser_NotFound(t *testing.T) {
	svc := &fakeAdminService{deactivateErr: ports.ErrUserNotFound}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusNotFound, "user_not_found")
}

func TestDeactivateUser_ServiceUnavailable(t *testing.T) {
	svc := &fakeAdminService{deactivateErr: ports.ErrAdminUnavailable}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service_unavailable")
}

func TestDeactivateUser_MissingID(t *testing.T) {
	svc := &fakeAdminService{}
	mux := newMux(svc)

	// Path ends exactly at the prefix with no trailing ID.
	req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid_uuid")
}

// ------------------------------------------------------------------
// Content-Type header
// ------------------------------------------------------------------

func TestHandlers_ContentType(t *testing.T) {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{Users: []user.User{}, Total: 0},
	}
	mux := newMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ------------------------------------------------------------------
// Wrapped errors
// ------------------------------------------------------------------

func TestDeactivateUser_WrappedErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "wrapped user not found",
			err:        errors.Join(errors.New("context"), ports.ErrUserNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "user_not_found",
		},
		{
			name:       "wrapped admin unavailable",
			err:        errors.Join(errors.New("context"), ports.ErrAdminUnavailable),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeAdminService{deactivateErr: tt.err}
			mux := newMux(svc)

			req := httptest.NewRequest(http.MethodDelete, "/_vibewarden/admin/users/550e8400-e29b-41d4-a716-446655440000", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertErrorResponse(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// assertErrorResponse checks the status code and the "error" field in the JSON body.
func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] != wantCode {
		t.Errorf("error code = %v, want %q", resp["error"], wantCode)
	}
}

// ------------------------------------------------------------------
// Config reload handler tests
// ------------------------------------------------------------------

// fakeReloader is a test double for ports.ConfigReloader.
type fakeReloader struct {
	reloadErr     error
	currentConfig ports.RedactedConfig
	reloadCalled  bool
	source        string
}

func (f *fakeReloader) Reload(_ context.Context, source string) error {
	f.reloadCalled = true
	f.source = source
	return f.reloadErr
}

func (f *fakeReloader) CurrentConfig() ports.RedactedConfig {
	if f.currentConfig == nil {
		// Use capitalised keys to match Go struct JSON marshalling (no json tags on Config).
		return ports.RedactedConfig{
			"Server": map[string]any{"Host": "127.0.0.1", "Port": float64(8443)},
			"Admin":  map[string]any{"Enabled": true, "Token": "[REDACTED]"},
		}
	}
	return f.currentConfig
}

func newMuxWithReloader(svc ports.AdminService, reloader ports.ConfigReloader) *http.ServeMux {
	mux := http.NewServeMux()
	h := vibehttp.NewAdminHandlers(svc, nil).WithReloader(reloader)
	h.RegisterRoutes(mux)
	return mux
}

func TestReloadConfig_Success(t *testing.T) {
	reloader := &fakeReloader{}
	mux := newMuxWithReloader(&fakeAdminService{}, reloader)

	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/config/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp ports.ReloadResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true")
	}
	if resp.Message == "" {
		t.Error("Message should not be empty on success")
	}
	if !reloader.reloadCalled {
		t.Error("Reload was not called on the reloader")
	}
	if reloader.source != "admin_api" {
		t.Errorf("source = %q, want admin_api", reloader.source)
	}
}

func TestReloadConfig_ReloaderError(t *testing.T) {
	reloader := &fakeReloader{reloadErr: errors.New("proxy reload failed")}
	mux := newMuxWithReloader(&fakeAdminService{}, reloader)

	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/config/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var resp ports.ReloadResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false")
	}
	if resp.Message == "" {
		t.Error("Message should describe the error")
	}
}

func TestReloadConfig_NoReloader(t *testing.T) {
	mux := newMux(&fakeAdminService{})

	req := httptest.NewRequest(http.MethodPost, "/_vibewarden/config/reload", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestGetConfig_Success(t *testing.T) {
	reloader := &fakeReloader{}
	mux := newMuxWithReloader(&fakeAdminService{}, reloader)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify sensitive field is redacted in the response.
	adminMap, ok := resp["Admin"].(map[string]any)
	if !ok {
		t.Fatal("Admin field missing from response")
	}
	if adminMap["Token"] != "[REDACTED]" {
		t.Errorf("Admin.Token = %v, want [REDACTED]", adminMap["Token"])
	}
}

func TestGetConfig_NoReloader(t *testing.T) {
	mux := newMux(&fakeAdminService{})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
