package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultPerPage is the number of items returned per page when the caller
	// does not supply a per_page query parameter.
	defaultPerPage = 20

	// maxPerPage caps the per_page query parameter to prevent excessively large
	// responses from the identity provider.
	maxPerPage = 100
)

// AdminHandlers holds the HTTP handler functions for the admin user management API.
// All routes are registered under the /_vibewarden/admin/ prefix.
type AdminHandlers struct {
	svc    ports.AdminService
	logger *slog.Logger
}

// NewAdminHandlers creates a new AdminHandlers backed by the supplied service.
// logger is used to record internal errors that must not be exposed to clients;
// pass slog.Default() when no custom logger is available.
func NewAdminHandlers(svc ports.AdminService, logger *slog.Logger) *AdminHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminHandlers{svc: svc, logger: logger}
}

// RegisterRoutes registers all admin routes on mux using the Go 1.22+
// method-prefixed pattern syntax ("METHOD /path").
// All routes are relative to the /_vibewarden/admin prefix.
func (h *AdminHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /_vibewarden/admin/users", h.listUsers)
	mux.HandleFunc("POST /_vibewarden/admin/users", h.inviteUser)
	// Go 1.22 ServeMux does not support path parameters via wildcards in the
	// same way as chi/gorilla. We register the wildcard pattern and extract
	// the ID segment manually.
	mux.HandleFunc("GET /_vibewarden/admin/users/", h.getUser)
	mux.HandleFunc("DELETE /_vibewarden/admin/users/", h.deactivateUser)
}

// ------------------------------------------------------------------
// Request / response types
// ------------------------------------------------------------------

// userResponse is the JSON representation of a single user returned by the API.
type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// listUsersResponse is the JSON body returned by GET /_vibewarden/admin/users.
type listUsersResponse struct {
	Users   []userResponse `json:"users"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}

// inviteUserRequest is the JSON body expected by POST /_vibewarden/admin/users.
type inviteUserRequest struct {
	Email string `json:"email"`
}

// inviteUserResponse is the JSON body returned by POST /_vibewarden/admin/users.
type inviteUserResponse struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	RecoveryLink string `json:"recovery_link"`
}

// errorResponse is the JSON body returned for all error responses.
type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// ------------------------------------------------------------------
// Handlers
// ------------------------------------------------------------------

// listUsers handles GET /_vibewarden/admin/users.
// Query parameters: page (default 1), per_page (default 20, max 100).
func (h *AdminHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	page := parseIntQuery(r, "page", 1)
	if page < 1 {
		page = 1
	}
	perPage := parseIntQuery(r, "per_page", defaultPerPage)
	if perPage < 1 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}

	result, err := h.svc.ListUsers(r.Context(), ports.Pagination{
		Page:    page,
		PerPage: perPage,
	})
	if err != nil {
		if errors.Is(err, ports.ErrAdminUnavailable) {
			h.logger.Error("admin service unavailable listing users", "err", err)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
			return
		}
		h.logger.Error("unexpected error listing users", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := listUsersResponse{
		Users:   make([]userResponse, 0, len(result.Users)),
		Total:   result.Total,
		Page:    page,
		PerPage: perPage,
	}
	for _, u := range result.Users {
		resp.Users = append(resp.Users, toUserResponse(u))
	}

	writeJSON(w, http.StatusOK, resp)
}

// getUser handles GET /_vibewarden/admin/users/{id}.
func (h *AdminHandlers) getUser(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/_vibewarden/admin/users/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID is required")
		return
	}

	if err := ValidateUUID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID must be a valid UUID")
		return
	}

	u, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		if errors.Is(err, ports.ErrInvalidUUID) {
			writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID must be a valid UUID")
			return
		}
		if errors.Is(err, ports.ErrAdminUnavailable) {
			h.logger.Error("admin service unavailable getting user", "err", err, "user_id", id)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
			return
		}
		h.logger.Error("unexpected error getting user", "err", err, "user_id", id)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(*u))
}

// inviteUser handles POST /_vibewarden/admin/users.
func (h *AdminHandlers) inviteUser(w http.ResponseWriter, r *http.Request) {
	var req inviteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON with an email field")
		return
	}

	normalised, err := ValidateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_email", "email address is invalid")
		return
	}

	result, err := h.svc.InviteUser(r.Context(), normalised, "")
	if err != nil {
		if errors.Is(err, ports.ErrUserAlreadyExists) {
			writeError(w, http.StatusConflict, "user_exists", "a user with this email already exists")
			return
		}
		if errors.Is(err, ports.ErrInvalidEmail) {
			writeError(w, http.StatusBadRequest, "invalid_email", "email address is invalid")
			return
		}
		if errors.Is(err, ports.ErrAdminUnavailable) {
			h.logger.Error("admin service unavailable inviting user", "err", err, "email", normalised)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
			return
		}
		h.logger.Error("unexpected error inviting user", "err", err, "email", normalised)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, inviteUserResponse{
		ID:           result.User.ID,
		Email:        result.User.Email,
		RecoveryLink: result.RecoveryLink,
	})
}

// deactivateUser handles DELETE /_vibewarden/admin/users/{id}.
func (h *AdminHandlers) deactivateUser(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/_vibewarden/admin/users/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID is required")
		return
	}

	if err := ValidateUUID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID must be a valid UUID")
		return
	}

	if err := h.svc.DeactivateUser(r.Context(), id, "", ""); err != nil {
		if errors.Is(err, ports.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		if errors.Is(err, ports.ErrInvalidUUID) {
			writeError(w, http.StatusBadRequest, "invalid_uuid", "user ID must be a valid UUID")
			return
		}
		if errors.Is(err, ports.ErrAdminUnavailable) {
			h.logger.Error("admin service unavailable deactivating user", "err", err, "user_id", id)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
			return
		}
		h.logger.Error("unexpected error deactivating user", "err", err, "user_id", id)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// toUserResponse converts a domain user.User to the wire representation.
func toUserResponse(u user.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Status:    string(u.Status),
		CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// parseIntQuery reads a named query parameter as an integer. Returns def if
// the parameter is missing or cannot be parsed.
func parseIntQuery(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

// extractIDFromPath extracts the final path segment after prefix.
// For example extractIDFromPath("/_vibewarden/admin/users/abc-123", "/_vibewarden/admin/users/")
// returns "abc-123".
func extractIDFromPath(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	return path[len(prefix):]
}

// writeJSON encodes v as JSON and writes it with the given status code.
// The Content-Type header is set to application/json.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeError writes a JSON error response with the given HTTP status code,
// machine-readable error code, and human-readable message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}
