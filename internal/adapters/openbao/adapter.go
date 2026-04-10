// Package openbao implements a VibeWarden SecretStore adapter that communicates
// with an OpenBao (open-source Vault fork) server via its HTTP API.
//
// The adapter uses no external Go modules — all HTTP calls are made with the
// stdlib net/http client. The KV v2 API is identical to HashiCorp Vault KV v2.
//
// Authentication is handled via AppRole: the adapter exchanges a role_id +
// secret_id pair for a short-lived client token and renews it automatically
// before it expires.
package openbao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthMethod selects how the adapter authenticates to OpenBao.
type AuthMethod string

const (
	// AuthMethodToken authenticates with a static root/service token.
	// Suitable for development (dev mode root token) and simple setups.
	AuthMethodToken AuthMethod = "token"

	// AuthMethodAppRole authenticates with a role_id + secret_id pair.
	// Recommended for production: least-privilege machine identity.
	AuthMethodAppRole AuthMethod = "approle"
)

// Config holds all settings needed to connect to and authenticate with OpenBao.
type Config struct {
	// Address is the OpenBao server URL (e.g. "http://openbao:8200").
	Address string

	// Auth selects the authentication method and its parameters.
	Auth AuthConfig

	// MountPath is the KV v2 mount path (default: "secret").
	// OpenBao KV v2 paths are: <MountPath>/data/<secret-path>
	MountPath string

	// TokenRenewGrace is how far before token expiry to trigger renewal.
	// Default: 10 seconds.
	TokenRenewGrace time.Duration
}

// AuthConfig holds the authentication credentials for OpenBao.
type AuthConfig struct {
	// Method selects the auth method: "token" or "approle".
	Method AuthMethod

	// Token is used when Method is "token".
	Token string

	// RoleID is the AppRole role_id. Used when Method is "approle".
	RoleID string

	// SecretID is the AppRole secret_id. Used when Method is "approle".
	SecretID string
}

// Adapter implements ports.SecretStore against OpenBao's HTTP API.
// It manages its own client token, renewing it automatically in the background.
type Adapter struct {
	cfg    Config
	client *http.Client
	logger *slog.Logger

	mu          sync.RWMutex
	token       string
	tokenExpiry time.Time
}

// New creates a new OpenBao Adapter. Call Init (or just start making API calls)
// to authenticate and obtain a token.
func New(cfg Config, logger *slog.Logger) *Adapter {
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}
	if cfg.TokenRenewGrace == 0 {
		cfg.TokenRenewGrace = 10 * time.Second
	}
	return &Adapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// Authenticate obtains (or re-uses) a client token from OpenBao.
// For AuthMethodToken it simply stores the static token.
// For AuthMethodAppRole it performs the AppRole login flow.
func (a *Adapter) Authenticate(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch a.cfg.Auth.Method {
	case AuthMethodToken, "":
		a.token = a.cfg.Auth.Token
		// Static tokens don't expire; set a far-future time so renewals are skipped.
		a.tokenExpiry = time.Now().Add(87600 * time.Hour)
		return nil
	case AuthMethodAppRole:
		return a.loginAppRole(ctx)
	default:
		return fmt.Errorf("openbao: unsupported auth method %q", a.cfg.Auth.Method)
	}
}

// loginAppRole exchanges a role_id + secret_id for a client token.
// Must be called with a.mu held for writing.
func (a *Adapter) loginAppRole(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{
		"role_id":   a.cfg.Auth.RoleID,
		"secret_id": a.cfg.Auth.SecretID,
	})
	if err != nil {
		return fmt.Errorf("openbao: marshal approle login: %w", err)
	}

	resp, err := a.doRaw(ctx, http.MethodPost, "/v1/auth/approle/login", "", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openbao: approle login: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openbao: approle login returned %d", resp.StatusCode)
	}

	var result struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("openbao: decode approle login response: %w", err)
	}
	if result.Auth.ClientToken == "" {
		return fmt.Errorf("openbao: approle login returned empty token")
	}

	a.token = result.Auth.ClientToken
	if result.Auth.LeaseDuration > 0 {
		a.tokenExpiry = time.Now().Add(time.Duration(result.Auth.LeaseDuration) * time.Second)
	} else {
		a.tokenExpiry = time.Now().Add(87600 * time.Hour)
	}
	a.logger.Info("openbao: approle login successful",
		slog.Time("token_expiry", a.tokenExpiry),
	)
	return nil
}

// ensureToken makes sure the adapter has a valid, non-expired token.
// If the token is close to expiry it attempts renewal, falling back to a
// full re-login if renewal fails.
func (a *Adapter) ensureToken(ctx context.Context) error {
	a.mu.RLock()
	needsRenewal := time.Until(a.tokenExpiry) < a.cfg.TokenRenewGrace
	a.mu.RUnlock()

	if !needsRenewal {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check under write lock (another goroutine may have renewed).
	if time.Until(a.tokenExpiry) >= a.cfg.TokenRenewGrace {
		return nil
	}

	// Try self-renewal first (only makes sense for AppRole tokens).
	if a.cfg.Auth.Method == AuthMethodAppRole && a.token != "" {
		if err := a.renewSelf(ctx); err == nil {
			return nil
		}
		// Renewal failed — fall through to full re-login.
		a.logger.Warn("openbao: token renewal failed, attempting re-login")
	}

	return a.loginAppRole(ctx)
}

// renewSelf calls POST /v1/auth/token/renew-self to extend the current token.
// Must be called with a.mu held for writing.
func (a *Adapter) renewSelf(ctx context.Context) error {
	resp, err := a.doRaw(ctx, http.MethodPost, "/v1/auth/token/renew-self", a.token, nil)
	if err != nil {
		return fmt.Errorf("openbao: renew-self: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openbao: renew-self returned %d", resp.StatusCode)
	}

	var result struct {
		Auth struct {
			LeaseDuration int `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("openbao: decode renew-self response: %w", err)
	}
	if result.Auth.LeaseDuration > 0 {
		a.tokenExpiry = time.Now().Add(time.Duration(result.Auth.LeaseDuration) * time.Second)
		a.logger.Info("openbao: token renewed", slog.Time("new_expiry", a.tokenExpiry))
	}
	return nil
}

// Get implements ports.SecretStore.
// It fetches the latest version of the secret at the KV v2 path.
func (a *Adapter) Get(ctx context.Context, path string) (map[string]string, error) {
	if err := a.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	apiPath := fmt.Sprintf("/v1/%s/data/%s", a.cfg.MountPath, path)
	resp, err := a.doRaw(ctx, http.MethodGet, apiPath, token, nil)
	if err != nil {
		return nil, fmt.Errorf("openbao: get %q: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("openbao: secret not found at %q", path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openbao: get %q returned %d", path, resp.StatusCode)
	}

	var result struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openbao: decode get response: %w", err)
	}

	// Convert map[string]any to map[string]string.
	out := make(map[string]string, len(result.Data.Data))
	for k, v := range result.Data.Data {
		switch sv := v.(type) {
		case string:
			out[k] = sv
		default:
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out, nil
}

// Put implements ports.SecretStore.
// It writes (or updates) a secret at the KV v2 path.
func (a *Adapter) Put(ctx context.Context, path string, data map[string]string) error {
	if err := a.ensureToken(ctx); err != nil {
		return fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	// Convert map[string]string to map[string]any for JSON encoding.
	anyData := make(map[string]any, len(data))
	for k, v := range data {
		anyData[k] = v
	}

	body, err := json.Marshal(map[string]any{"data": anyData})
	if err != nil {
		return fmt.Errorf("openbao: marshal put body: %w", err)
	}

	apiPath := fmt.Sprintf("/v1/%s/data/%s", a.cfg.MountPath, path)
	resp, err := a.doRaw(ctx, http.MethodPost, apiPath, token, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openbao: put %q: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("openbao: put %q returned %d", path, resp.StatusCode)
	}
	return nil
}

// Delete implements ports.SecretStore.
// It permanently deletes all versions at the KV v2 path via the metadata endpoint.
func (a *Adapter) Delete(ctx context.Context, path string) error {
	if err := a.ensureToken(ctx); err != nil {
		return fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	apiPath := fmt.Sprintf("/v1/%s/metadata/%s", a.cfg.MountPath, path)
	resp, err := a.doRaw(ctx, http.MethodDelete, apiPath, token, nil)
	if err != nil {
		return fmt.Errorf("openbao: delete %q: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openbao: delete %q returned %d", path, resp.StatusCode)
	}
	return nil
}

// List implements ports.SecretStore.
// It returns the keys beneath prefix using the KV v2 metadata list endpoint.
func (a *Adapter) List(ctx context.Context, prefix string) ([]string, error) {
	if err := a.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	apiPath := fmt.Sprintf("/v1/%s/metadata/%s", a.cfg.MountPath, prefix)
	resp, err := a.doRaw(ctx, "LIST", apiPath, token, nil)
	if err != nil {
		return nil, fmt.Errorf("openbao: list %q: %w", prefix, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openbao: list %q returned %d", prefix, resp.StatusCode)
	}

	var result struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openbao: decode list response: %w", err)
	}
	return result.Data.Keys, nil
}

// Health implements ports.SecretStore.
// It calls GET /v1/sys/health and returns nil when sealed=false.
func (a *Adapter) Health(ctx context.Context) error {
	resp, err := a.doRaw(ctx, http.MethodGet, "/v1/sys/health", "", nil)
	if err != nil {
		return fmt.Errorf("openbao: health check: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// OpenBao returns 200 when initialized+unsealed, 429 when standby,
	// 472 when DR secondary, 473 when performance standby, 501 when not initialized,
	// 503 when sealed. We accept 200 and 429 as "healthy".
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openbao: unhealthy (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// GetMetadata fetches the KV v2 metadata for a path.
// Returns the created_time and updated_time for secret health checks.
func (a *Adapter) GetMetadata(ctx context.Context, path string) (*SecretMetadata, error) {
	if err := a.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	apiPath := fmt.Sprintf("/v1/%s/metadata/%s", a.cfg.MountPath, path)
	resp, err := a.doRaw(ctx, http.MethodGet, apiPath, token, nil)
	if err != nil {
		return nil, fmt.Errorf("openbao: get metadata %q: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("openbao: metadata not found for %q", path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openbao: get metadata %q returned %d", path, resp.StatusCode)
	}

	var result struct {
		Data struct {
			CreatedTime    time.Time `json:"created_time"`
			UpdatedTime    time.Time `json:"updated_time"`
			CurrentVersion int       `json:"current_version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openbao: decode metadata response: %w", err)
	}
	return &SecretMetadata{
		CreatedTime:    result.Data.CreatedTime,
		UpdatedTime:    result.Data.UpdatedTime,
		CurrentVersion: result.Data.CurrentVersion,
	}, nil
}

// SecretMetadata holds the KV v2 metadata for a single secret path.
type SecretMetadata struct {
	// CreatedTime is when the secret was first created.
	CreatedTime time.Time

	// UpdatedTime is when the secret was last updated (latest version written).
	UpdatedTime time.Time

	// CurrentVersion is the latest version number of the secret.
	CurrentVersion int
}

// RequestDynamicCredentials requests short-lived Postgres credentials from
// OpenBao's database secret engine at the given role name.
func (a *Adapter) RequestDynamicCredentials(ctx context.Context, role string) (*DynamicCredentials, error) {
	if err := a.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	apiPath := fmt.Sprintf("/v1/database/creds/%s", role)
	resp, err := a.doRaw(ctx, http.MethodGet, apiPath, token, nil)
	if err != nil {
		return nil, fmt.Errorf("openbao: request dynamic credentials for role %q: %w", role, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openbao: dynamic credentials for role %q returned %d", role, resp.StatusCode)
	}

	var result struct {
		LeaseID       string `json:"lease_id"`
		LeaseDuration int    `json:"lease_duration"`
		Data          struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openbao: decode dynamic credentials response: %w", err)
	}
	if result.Data.Username == "" {
		return nil, fmt.Errorf("openbao: empty username in dynamic credentials for role %q", role)
	}

	return &DynamicCredentials{
		LeaseID:  result.LeaseID,
		Username: result.Data.Username,
		Password: result.Data.Password,
		TTL:      time.Duration(result.LeaseDuration) * time.Second,
		IssuedAt: time.Now(),
	}, nil
}

// DynamicCredentials holds short-lived credentials issued by OpenBao's database engine.
type DynamicCredentials struct {
	// LeaseID is the OpenBao lease identifier for renewal and revocation.
	LeaseID string

	// Username is the generated database username.
	Username string

	// Password is the generated database password.
	Password string

	// TTL is the lease duration.
	TTL time.Duration

	// IssuedAt is when the credentials were issued.
	IssuedAt time.Time
}

// ExpiresAt returns the time when these credentials expire.
func (d *DynamicCredentials) ExpiresAt() time.Time {
	return d.IssuedAt.Add(d.TTL)
}

// RenewLease renews an OpenBao lease by its ID.
func (a *Adapter) RenewLease(ctx context.Context, leaseID string, incrementSeconds int) (time.Duration, error) {
	if err := a.ensureToken(ctx); err != nil {
		return 0, fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	body, err := json.Marshal(map[string]any{
		"lease_id":  leaseID,
		"increment": incrementSeconds,
	})
	if err != nil {
		return 0, fmt.Errorf("openbao: marshal renew lease body: %w", err)
	}

	resp, err := a.doRaw(ctx, http.MethodPost, "/v1/sys/leases/renew", token, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("openbao: renew lease %q: %w", leaseID, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("openbao: renew lease %q returned %d", leaseID, resp.StatusCode)
	}

	var result struct {
		LeaseDuration int `json:"lease_duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("openbao: decode renew lease response: %w", err)
	}
	return time.Duration(result.LeaseDuration) * time.Second, nil
}

// RevokeLease revokes an OpenBao lease immediately.
func (a *Adapter) RevokeLease(ctx context.Context, leaseID string) error {
	if err := a.ensureToken(ctx); err != nil {
		return fmt.Errorf("openbao: ensure token: %w", err)
	}

	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()

	body, err := json.Marshal(map[string]string{"lease_id": leaseID})
	if err != nil {
		return fmt.Errorf("openbao: marshal revoke lease body: %w", err)
	}

	resp, err := a.doRaw(ctx, http.MethodPost, "/v1/sys/leases/revoke", token, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openbao: revoke lease %q: %w", leaseID, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openbao: revoke lease %q returned %d", leaseID, resp.StatusCode)
	}
	return nil
}

// doRaw performs a raw HTTP request against the OpenBao server.
// token may be empty for unauthenticated calls (e.g. health check, AppRole login).
func (a *Adapter) doRaw(ctx context.Context, method, path, token string, body io.Reader) (*http.Response, error) {
	url := a.cfg.Address + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, path, err)
	}
	return resp, nil
}
