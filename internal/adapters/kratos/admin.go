package kratos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// kratosAdminIdentity mirrors the identity object returned by the Kratos admin API.
type kratosAdminIdentity struct {
	ID        string         `json:"id"`
	State     string         `json:"state"`
	Traits    map[string]any `json:"traits"`
	CreatedAt string         `json:"created_at"`
}

// kratosCreateIdentityRequest is the body sent to POST /admin/identities.
type kratosCreateIdentityRequest struct {
	SchemaID string         `json:"schema_id"`
	Traits   map[string]any `json:"traits"`
}

// kratosPatchIdentityRequest is the body sent to PATCH /admin/identities/:id.
type kratosPatchIdentityRequest struct {
	State string `json:"state"`
}

// kratosRecoveryLinkRequest is the body sent to POST /admin/recovery/link.
type kratosRecoveryLinkRequest struct {
	IdentityID string `json:"identity_id"`
}

// kratosRecoveryLinkResponse mirrors the response from POST /admin/recovery/link.
type kratosRecoveryLinkResponse struct {
	RecoveryLink string `json:"recovery_link"`
}

// AdminAdapter implements ports.UserAdmin using the Ory Kratos admin API.
// It communicates over plain net/http, matching the pattern of the existing
// Adapter for the public API — no kratos-client-go is used.
type AdminAdapter struct {
	adminURL string
	client   *http.Client
	logger   *slog.Logger
}

// NewAdminAdapter creates a new AdminAdapter.
// adminURL is the base URL of the Kratos admin API (e.g. "http://localhost:4434").
// Pass zero for timeout to use the default of 5 seconds.
func NewAdminAdapter(adminURL string, timeout time.Duration, logger *slog.Logger) *AdminAdapter {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &AdminAdapter{
		adminURL: adminURL,
		client:   &http.Client{Timeout: timeout},
		logger:   logger,
	}
}

// ListUsers implements ports.UserAdmin.
// It calls GET /admin/identities with per_page and page query parameters.
func (a *AdminAdapter) ListUsers(ctx context.Context, pagination ports.Pagination) (*ports.PaginatedUsers, error) {
	page := pagination.Page
	if page < 1 {
		page = 1
	}
	perPage := pagination.PerPage
	if perPage < 1 {
		perPage = 25
	}

	endpoint := fmt.Sprintf("%s/admin/identities?page=%d&per_page=%d", a.adminURL, page, perPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building list identities request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, a.wrapNetworkError(ctx, "list identities", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if err := a.checkAdminError(ctx, resp, "list identities"); err != nil {
		return nil, err
	}

	var identities []kratosAdminIdentity
	if err := json.NewDecoder(resp.Body).Decode(&identities); err != nil {
		return nil, fmt.Errorf("decoding list identities response: %w: %w", err, ports.ErrAdminUnavailable)
	}

	users := make([]user.User, 0, len(identities))
	for _, id := range identities {
		users = append(users, mapIdentityToUser(id))
	}

	return &ports.PaginatedUsers{
		Users: users,
		Total: -1, // Kratos list endpoint does not return a total count.
	}, nil
}

// GetUser implements ports.UserAdmin.
// It calls GET /admin/identities/:id.
func (a *AdminAdapter) GetUser(ctx context.Context, id string) (*user.User, error) {
	endpoint := fmt.Sprintf("%s/admin/identities/%s", a.adminURL, url.PathEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building get identity request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, a.wrapNetworkError(ctx, "get identity", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode == http.StatusNotFound {
		return nil, ports.ErrUserNotFound
	}

	if err := a.checkAdminError(ctx, resp, "get identity"); err != nil {
		return nil, err
	}

	var identity kratosAdminIdentity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return nil, fmt.Errorf("decoding get identity response: %w: %w", err, ports.ErrAdminUnavailable)
	}

	u := mapIdentityToUser(identity)
	return &u, nil
}

// InviteUser implements ports.UserAdmin.
// It calls POST /admin/identities to create the identity, then
// POST /admin/recovery/link to obtain a one-time recovery link.
func (a *AdminAdapter) InviteUser(ctx context.Context, email string) (*ports.InviteResult, error) {
	// Step 1: Create the identity.
	createBody := kratosCreateIdentityRequest{
		SchemaID: "default",
		Traits:   map[string]any{"email": email},
	}
	bodyBytes, err := json.Marshal(createBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling create identity request: %w", err)
	}

	createEndpoint := fmt.Sprintf("%s/admin/identities", a.adminURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("building create identity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, a.wrapNetworkError(ctx, "create identity", createEndpoint, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode == http.StatusConflict {
		return nil, ports.ErrUserAlreadyExists
	}

	if err := a.checkAdminError(ctx, resp, "create identity"); err != nil {
		return nil, err
	}

	var created kratosAdminIdentity
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decoding create identity response: %w: %w", err, ports.ErrAdminUnavailable)
	}

	// Step 2: Generate a one-time recovery link.
	recoveryLink, err := a.generateRecoveryLink(ctx, created.ID)
	if err != nil {
		// Recovery link failure is non-fatal — we return the user but log the error.
		// The admin can re-trigger a recovery link separately.
		a.logger.WarnContext(ctx, "failed to generate recovery link after user creation",
			slog.String("identity_id", created.ID),
			slog.String("error", err.Error()),
		)
		recoveryLink = ""
	}

	u := mapIdentityToUser(created)
	return &ports.InviteResult{
		User:         u,
		RecoveryLink: recoveryLink,
	}, nil
}

// DeactivateUser implements ports.UserAdmin.
// It calls PATCH /admin/identities/:id with state=inactive.
func (a *AdminAdapter) DeactivateUser(ctx context.Context, id string) error {
	patchBody := kratosPatchIdentityRequest{State: "inactive"}
	bodyBytes, err := json.Marshal(patchBody)
	if err != nil {
		return fmt.Errorf("marshalling patch identity request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/admin/identities/%s", a.adminURL, url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("building patch identity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return a.wrapNetworkError(ctx, "deactivate identity", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode == http.StatusNotFound {
		return ports.ErrUserNotFound
	}

	if err := a.checkAdminError(ctx, resp, "deactivate identity"); err != nil {
		return err
	}

	return nil
}

// generateRecoveryLink calls POST /admin/recovery/link for the given identity ID
// and returns the one-time link.
func (a *AdminAdapter) generateRecoveryLink(ctx context.Context, identityID string) (string, error) {
	body := kratosRecoveryLinkRequest{IdentityID: identityID}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling recovery link request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/admin/recovery/link", a.adminURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("building recovery link request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", a.wrapNetworkError(ctx, "recovery link", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // response body close error is not actionable

	if err := a.checkAdminError(ctx, resp, "recovery link"); err != nil {
		return "", err
	}

	var linkResp kratosRecoveryLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&linkResp); err != nil {
		return "", fmt.Errorf("decoding recovery link response: %w", err)
	}

	return linkResp.RecoveryLink, nil
}

// checkAdminError inspects resp.StatusCode and returns the appropriate sentinel
// error for non-success responses. It is a no-op for 2xx responses.
func (a *AdminAdapter) checkAdminError(ctx context.Context, resp *http.Response, operation string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode >= 500 {
		a.logger.ErrorContext(ctx, "kratos admin API server error",
			slog.String("operation", operation),
			slog.Int("status", resp.StatusCode),
		)
		return fmt.Errorf("kratos admin %s responded with %d: %w", operation, resp.StatusCode, ports.ErrAdminUnavailable)
	}

	// Unexpected 4xx (other than 404 and 409, which callers handle before calling here).
	return fmt.Errorf("kratos admin %s unexpected status %d: %w", operation, resp.StatusCode, ports.ErrAdminUnavailable)
}

// wrapNetworkError wraps a network-level error from the admin API into ErrAdminUnavailable.
func (a *AdminAdapter) wrapNetworkError(ctx context.Context, operation, endpoint string, err error) error {
	a.logger.ErrorContext(ctx, "kratos admin API request failed",
		slog.String("operation", operation),
		slog.String("url", endpoint),
		slog.String("error", err.Error()),
	)
	return fmt.Errorf("kratos admin %s unreachable: %w", operation, ports.ErrAdminUnavailable)
}

// mapIdentityToUser converts a kratosAdminIdentity to a domain user.User.
func mapIdentityToUser(id kratosAdminIdentity) user.User {
	status := user.StatusActive
	if id.State == "inactive" {
		status = user.StatusInactive
	}

	var email string
	if e, ok := id.Traits["email"].(string); ok {
		email = e
	}

	createdAt := time.Time{}
	if id.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, id.CreatedAt); err == nil {
			createdAt = t
		}
	}

	return user.User{
		ID:        id.ID,
		Email:     email,
		Status:    status,
		CreatedAt: createdAt,
	}
}
