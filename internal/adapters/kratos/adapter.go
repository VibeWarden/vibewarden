// Package kratos implements the SessionChecker port using Ory Kratos.
// It communicates with the Kratos public API over plain HTTP, avoiding
// the heavy transitive dependencies of the Ory Kratos Go client library.
package kratos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultTimeout is the HTTP client timeout when calling Kratos.
	defaultTimeout = 5 * time.Second

	// whoamiPath is the Kratos endpoint for session validation.
	whoamiPath = "/sessions/whoami"
)

// kratosSessionResponse mirrors the relevant fields from the Kratos
// GET /sessions/whoami JSON response.
type kratosSessionResponse struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`

	AuthenticatedAt string `json:"authenticated_at"`
	ExpiresAt       string `json:"expires_at"`

	Identity kratosIdentityResponse `json:"identity"`
}

// kratosIdentityResponse mirrors the identity portion of the Kratos session response.
type kratosIdentityResponse struct {
	ID     string         `json:"id"`
	Traits map[string]any `json:"traits"`

	// VerifiableAddresses holds the list of verifiable contact addresses.
	VerifiableAddresses []kratosVerifiableAddress `json:"verifiable_addresses"`
}

// kratosVerifiableAddress mirrors one entry in verifiable_addresses.
type kratosVerifiableAddress struct {
	Value    string `json:"value"`
	Via      string `json:"via"`
	Verified bool   `json:"verified"`
}

// Adapter implements ports.SessionChecker using the Ory Kratos public API.
type Adapter struct {
	publicURL string
	client    *http.Client
	logger    *slog.Logger
}

// NewAdapter creates a new Kratos adapter.
// publicURL is the base URL of the Kratos public API (e.g. "http://localhost:4433").
// The HTTP client timeout defaults to 5 seconds; pass a non-zero timeout to override.
func NewAdapter(publicURL string, timeout time.Duration, logger *slog.Logger) *Adapter {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &Adapter{
		publicURL: publicURL,
		client:    &http.Client{Timeout: timeout},
		logger:    logger,
	}
}

// CheckSession implements ports.SessionChecker.
// It calls the Kratos GET /sessions/whoami endpoint, passing the session cookie
// in the Cookie request header, and maps the response to a ports.Session.
//
// Error semantics:
//   - Returns ports.ErrSessionNotFound when no session cookie is present.
//   - Returns ports.ErrSessionInvalid when Kratos responds with 401 or the session is inactive.
//   - Returns ports.ErrAuthProviderUnavailable when Kratos cannot be reached or returns 5xx.
func (a *Adapter) CheckSession(ctx context.Context, sessionCookie string) (*ports.Session, error) {
	if sessionCookie == "" {
		return nil, ports.ErrSessionNotFound
	}

	url := a.publicURL + whoamiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building whoami request: %w", err)
	}
	req.Header.Set("Cookie", sessionCookie)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		// Network-level failure (connection refused, timeout, DNS failure, etc.).
		// Treat as provider unavailable — fail closed.
		a.logger.ErrorContext(ctx, "kratos whoami request failed",
			slog.String("url", url),
			slog.String("error", err.Error()),
		)

		if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
			return nil, fmt.Errorf("kratos request timed out: %w", ports.ErrAuthProviderUnavailable)
		}
		return nil, fmt.Errorf("kratos unreachable: %w", ports.ErrAuthProviderUnavailable)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through to parse

	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ports.ErrSessionInvalid

	case resp.StatusCode >= 500:
		a.logger.ErrorContext(ctx, "kratos returned server error",
			slog.String("url", url),
			slog.Int("status", resp.StatusCode),
		)
		return nil, fmt.Errorf("kratos responded with %d: %w", resp.StatusCode, ports.ErrAuthProviderUnavailable)

	default:
		return nil, fmt.Errorf("unexpected kratos status %d: %w", resp.StatusCode, ports.ErrSessionInvalid)
	}

	var kratosResp kratosSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&kratosResp); err != nil {
		return nil, fmt.Errorf("decoding kratos response: %w", err)
	}

	if !kratosResp.Active {
		return nil, ports.ErrSessionInvalid
	}

	return mapSession(kratosResp), nil
}

// mapSession converts a kratosSessionResponse into a ports.Session.
func mapSession(kr kratosSessionResponse) *ports.Session {
	identity := ports.Identity{
		ID:     kr.Identity.ID,
		Traits: kr.Identity.Traits,
	}

	// Extract email and verified status from verifiable_addresses.
	for _, addr := range kr.Identity.VerifiableAddresses {
		if addr.Via == "email" {
			identity.Email = addr.Value
			identity.EmailVerified = addr.Verified
			break
		}
	}

	// Fall back to traits["email"] when verifiable_addresses is absent.
	if identity.Email == "" {
		if email, ok := kr.Identity.Traits["email"].(string); ok {
			identity.Email = email
		}
	}

	return &ports.Session{
		ID:              kr.ID,
		Identity:        identity,
		Active:          kr.Active,
		AuthenticatedAt: kr.AuthenticatedAt,
		ExpiresAt:       kr.ExpiresAt,
	}
}

// isTimeoutError reports whether the error is a timeout at the transport layer.
func isTimeoutError(err error) bool {
	type timeoutErr interface {
		Timeout() bool
	}
	var te timeoutErr
	return errors.As(err, &te) && te.Timeout()
}
