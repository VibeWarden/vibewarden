# ADR-023: JWT/OIDC Identity Adapter

**Date**: 2026-03-28
**Issue**: #384
**Status**: Accepted

### Context

Epic #373 ("Flexible Auth") requires VibeWarden to support JWT/OIDC authentication
in addition to Ory Kratos sessions. Many users already have an external identity
provider (Auth0, Okta, Keycloak, Azure AD, Google) and want to use VibeWarden as
a security sidecar without running Kratos.

ADR-022 introduced the `IdentityProvider` port interface that abstracts authentication
mechanisms. This ADR designs the JWT/OIDC adapter that implements that port.

**Requirements from issue #384:**

- Implement `ports.IdentityProvider` for JWT validation
- JWKS fetching with local caching and automatic key rotation
- Support RS256 and ES256 signature algorithms (minimum)
- Standard claim validation: `exp`, `iat`, `nbf`, `iss`, `aud`
- Claims-to-headers mapping (e.g., `sub` -> `X-User-Id`)
- OIDC Discovery support (auto-detect JWKS URL from `/.well-known/openid-configuration`)
- Structured log events for JWT validation outcomes

### Decision

Create a JWT adapter in `internal/adapters/jwt/` that:

1. Fetches JWKS from the configured endpoint (or discovers it via OIDC)
2. Caches keys with background refresh
3. Validates JWT signatures, timestamps, issuer, and audience
4. Maps claims to a domain `Identity` value object
5. Supports configurable claims-to-headers mapping

#### Domain Model Changes

**No new domain types.** The existing `identity.Identity` and `identity.AuthResult`
from ADR-022 are sufficient. The JWT adapter will:

- Set `Identity.ID()` from the `sub` claim
- Set `Identity.Email()` from the `email` claim (if present)
- Set `Identity.EmailVerified()` from the `email_verified` claim (if present, default false)
- Set `Identity.Provider()` to `"jwt"`
- Store all non-reserved claims in `Identity.Claims()`

Reserved claims excluded from `Claims()`: `sub`, `iss`, `aud`, `exp`, `iat`, `nbf`, `jti`, `typ`.

#### Ports (Interfaces)

**No new port interfaces.** The JWT adapter implements the existing `ports.IdentityProvider`
interface defined in ADR-022:

```go
// internal/ports/identity.go (existing)
type IdentityProvider interface {
    Name() string
    Authenticate(ctx context.Context, r *http.Request) identity.AuthResult
}
```

**New port interface for JWKS fetching:**

A new port is needed to abstract the JWKS HTTP client, enabling testability without
hitting real endpoints.

**File: `internal/ports/jwks.go`**

```go
package ports

import (
    "context"

    "github.com/go-jose/go-jose/v4"
)

// JWKSFetcher retrieves JSON Web Key Sets from a remote endpoint.
// Implementations handle HTTP transport, caching, and key rotation.
type JWKSFetcher interface {
    // FetchKeys retrieves the current JWKS from the configured endpoint.
    // The implementation should cache keys and refresh them periodically or
    // when a key is not found (key rotation scenario).
    //
    // Returns the JWKS or an error if the endpoint cannot be reached.
    FetchKeys(ctx context.Context) (*jose.JSONWebKeySet, error)

    // GetKey retrieves a specific key by key ID (kid).
    // If the key is not in the cache, the implementation should attempt a
    // refresh before returning an error.
    GetKey(ctx context.Context, kid string) (*jose.JSONWebKey, error)
}
```

#### Adapters

**New adapter: `internal/adapters/jwt/adapter.go`**

The JWT adapter implements `ports.IdentityProvider` using go-jose/v4 for JWT
parsing and signature validation.

```go
package jwt

import (
    "context"
    "errors"
    "log/slog"
    "net/http"
    "strings"
    "time"

    "github.com/go-jose/go-jose/v4"
    josejwt "github.com/go-jose/go-jose/v4/jwt"

    "github.com/vibewarden/vibewarden/internal/domain/identity"
    "github.com/vibewarden/vibewarden/internal/ports"
)

// Config holds JWT adapter configuration.
type Config struct {
    // JWKSURL is the URL to fetch the JWKS from.
    // Either JWKSURL or IssuerURL must be set.
    JWKSURL string

    // IssuerURL is the OIDC issuer URL for discovery.
    // When set, JWKS URL is discovered from /.well-known/openid-configuration.
    // If JWKSURL is also set, JWKSURL takes precedence.
    IssuerURL string

    // Issuer is the expected "iss" claim value. Required.
    Issuer string

    // Audience is the expected "aud" claim value. Required.
    Audience string

    // ClaimsToHeaders maps JWT claim names to HTTP header names.
    // Example: {"sub": "X-User-Id", "email": "X-User-Email"}
    // The middleware injects these headers into the request forwarded to upstream.
    ClaimsToHeaders map[string]string

    // AllowedAlgorithms restricts which signing algorithms are accepted.
    // Default: ["RS256", "ES256"] if empty.
    AllowedAlgorithms []string

    // CacheTTL is how long to cache the JWKS before refreshing.
    // Default: 1 hour.
    CacheTTL time.Duration

    // RefreshOnMissingKey controls whether to refresh JWKS when a key ID is
    // not found (supports key rotation). Default: true.
    RefreshOnMissingKey bool
}

// Adapter implements ports.IdentityProvider for JWT/OIDC tokens.
type Adapter struct {
    config  Config
    fetcher ports.JWKSFetcher
    logger  *slog.Logger
}

// NewAdapter creates a new JWT identity adapter.
func NewAdapter(cfg Config, fetcher ports.JWKSFetcher, logger *slog.Logger) (*Adapter, error) {
    // Validate configuration
    if cfg.JWKSURL == "" && cfg.IssuerURL == "" {
        return nil, errors.New("jwt: either jwks_url or issuer_url is required")
    }
    if cfg.Issuer == "" {
        return nil, errors.New("jwt: issuer is required")
    }
    if cfg.Audience == "" {
        return nil, errors.New("jwt: audience is required")
    }
    if len(cfg.AllowedAlgorithms) == 0 {
        cfg.AllowedAlgorithms = []string{"RS256", "ES256"}
    }
    if cfg.CacheTTL == 0 {
        cfg.CacheTTL = time.Hour
    }

    return &Adapter{
        config:  cfg,
        fetcher: fetcher,
        logger:  logger,
    }, nil
}

// Name implements ports.IdentityProvider.
func (a *Adapter) Name() string { return "jwt" }

// Authenticate implements ports.IdentityProvider.
// It extracts the Bearer token from the Authorization header, validates it,
// and returns an AuthResult with the user's identity.
func (a *Adapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
    // Extract Bearer token
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        return identity.Failure("no_credentials", "no Authorization header")
    }
    if !strings.HasPrefix(authHeader, "Bearer ") {
        return identity.Failure("no_credentials", "Authorization header is not Bearer")
    }
    rawToken := strings.TrimPrefix(authHeader, "Bearer ")
    if rawToken == "" {
        return identity.Failure("no_credentials", "empty Bearer token")
    }

    // Parse the token (without verification first to get the key ID)
    tok, err := josejwt.ParseSigned(rawToken, a.config.AllowedAlgorithms)
    if err != nil {
        return identity.Failure("invalid_token", "failed to parse JWT: "+err.Error())
    }

    // Get the signing key
    if len(tok.Headers) == 0 {
        return identity.Failure("invalid_token", "JWT has no headers")
    }
    kid := tok.Headers[0].KeyID

    key, err := a.fetcher.GetKey(ctx, kid)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
            return identity.Failure("provider_unavailable", "JWKS fetch timed out")
        }
        return identity.Failure("invalid_signature", "key not found: "+kid)
    }

    // Verify signature and extract claims
    var claims josejwt.Claims
    var customClaims map[string]any
    if err := tok.Claims(key.Key, &claims, &customClaims); err != nil {
        return identity.Failure("invalid_signature", "signature verification failed: "+err.Error())
    }

    // Validate standard claims
    expected := josejwt.Expected{
        Issuer:   a.config.Issuer,
        Audience: josejwt.Audience{a.config.Audience},
        Time:     time.Now(),
    }
    if err := claims.Validate(expected); err != nil {
        reason := "token_invalid"
        if strings.Contains(err.Error(), "expired") {
            reason = "token_expired"
        } else if strings.Contains(err.Error(), "not yet valid") {
            reason = "token_not_yet_valid"
        } else if strings.Contains(err.Error(), "issuer") {
            reason = "invalid_issuer"
        } else if strings.Contains(err.Error(), "audience") {
            reason = "invalid_audience"
        }
        return identity.Failure(reason, err.Error())
    }

    // Build Identity
    email, _ := customClaims["email"].(string)
    emailVerified, _ := customClaims["email_verified"].(bool)

    // Filter out reserved claims from the claims map
    reservedClaims := map[string]bool{
        "sub": true, "iss": true, "aud": true, "exp": true,
        "iat": true, "nbf": true, "jti": true, "typ": true,
    }
    filteredClaims := make(map[string]any)
    for k, v := range customClaims {
        if !reservedClaims[k] {
            filteredClaims[k] = v
        }
    }

    ident, err := identity.NewIdentity(
        claims.Subject,
        email,
        "jwt",
        emailVerified,
        filteredClaims,
    )
    if err != nil {
        return identity.Failure("invalid_identity", err.Error())
    }

    return identity.Success(ident)
}
```

**New adapter: `internal/adapters/jwt/jwks_fetcher.go`**

The JWKS fetcher handles HTTP transport, caching, and key rotation.

```go
package jwt

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "sync"
    "time"

    "github.com/go-jose/go-jose/v4"
)

// HTTPJWKSFetcher implements ports.JWKSFetcher using HTTP.
type HTTPJWKSFetcher struct {
    jwksURL  string
    client   *http.Client
    logger   *slog.Logger
    cacheTTL time.Duration

    mu        sync.RWMutex
    cache     *jose.JSONWebKeySet
    cachedAt  time.Time
    refreshMu sync.Mutex // Prevents concurrent refresh requests
}

// NewHTTPJWKSFetcher creates a fetcher for the given JWKS URL.
func NewHTTPJWKSFetcher(jwksURL string, timeout time.Duration, cacheTTL time.Duration, logger *slog.Logger) *HTTPJWKSFetcher {
    if timeout == 0 {
        timeout = 10 * time.Second
    }
    if cacheTTL == 0 {
        cacheTTL = time.Hour
    }
    return &HTTPJWKSFetcher{
        jwksURL:  jwksURL,
        client:   &http.Client{Timeout: timeout},
        logger:   logger,
        cacheTTL: cacheTTL,
    }
}

// FetchKeys retrieves the current JWKS, using cache if valid.
func (f *HTTPJWKSFetcher) FetchKeys(ctx context.Context) (*jose.JSONWebKeySet, error) {
    f.mu.RLock()
    if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
        jwks := f.cache
        f.mu.RUnlock()
        return jwks, nil
    }
    f.mu.RUnlock()

    return f.refresh(ctx)
}

// GetKey retrieves a specific key by ID, refreshing if not found.
func (f *HTTPJWKSFetcher) GetKey(ctx context.Context, kid string) (*jose.JSONWebKey, error) {
    jwks, err := f.FetchKeys(ctx)
    if err != nil {
        return nil, err
    }

    keys := jwks.Key(kid)
    if len(keys) > 0 {
        return &keys[0], nil
    }

    // Key not found — try a refresh (handles key rotation)
    jwks, err = f.refresh(ctx)
    if err != nil {
        return nil, err
    }

    keys = jwks.Key(kid)
    if len(keys) > 0 {
        return &keys[0], nil
    }

    return nil, fmt.Errorf("key not found: %s", kid)
}

func (f *HTTPJWKSFetcher) refresh(ctx context.Context) (*jose.JSONWebKeySet, error) {
    f.refreshMu.Lock()
    defer f.refreshMu.Unlock()

    // Double-check: another goroutine may have refreshed while we waited for the lock.
    f.mu.RLock()
    if f.cache != nil && time.Since(f.cachedAt) < f.cacheTTL {
        jwks := f.cache
        f.mu.RUnlock()
        return jwks, nil
    }
    f.mu.RUnlock()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.jwksURL, nil)
    if err != nil {
        return nil, fmt.Errorf("creating JWKS request: %w", err)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := f.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("fetching JWKS: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
    if err != nil {
        return nil, fmt.Errorf("reading JWKS response: %w", err)
    }

    var jwks jose.JSONWebKeySet
    if err := json.Unmarshal(body, &jwks); err != nil {
        return nil, fmt.Errorf("parsing JWKS: %w", err)
    }

    f.mu.Lock()
    f.cache = &jwks
    f.cachedAt = time.Now()
    f.mu.Unlock()

    f.logger.Info("JWKS cache refreshed",
        slog.String("url", f.jwksURL),
        slog.Int("key_count", len(jwks.Keys)),
    )

    return &jwks, nil
}
```

**New adapter: `internal/adapters/jwt/discovery.go`**

OIDC Discovery support for auto-detecting JWKS URL.

```go
package jwt

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

// OIDCConfiguration is the subset of OpenID Connect Discovery response we need.
type OIDCConfiguration struct {
    JwksURI string `json:"jwks_uri"`
    Issuer  string `json:"issuer"`
}

// DiscoverJWKSURL fetches the OIDC configuration and returns the JWKS URI.
func DiscoverJWKSURL(ctx context.Context, issuerURL string, timeout time.Duration) (string, error) {
    if timeout == 0 {
        timeout = 10 * time.Second
    }

    // Normalize issuer URL
    issuerURL = strings.TrimSuffix(issuerURL, "/")
    discoveryURL := issuerURL + "/.well-known/openid-configuration"

    client := &http.Client{Timeout: timeout}
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
    if err != nil {
        return "", fmt.Errorf("creating discovery request: %w", err)
    }
    req.Header.Set("Accept", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("fetching OIDC configuration: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("OIDC discovery endpoint returned %d", resp.StatusCode)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
    if err != nil {
        return "", fmt.Errorf("reading OIDC configuration: %w", err)
    }

    var config OIDCConfiguration
    if err := json.Unmarshal(body, &config); err != nil {
        return "", fmt.Errorf("parsing OIDC configuration: %w", err)
    }

    if config.JwksURI == "" {
        return "", fmt.Errorf("OIDC configuration missing jwks_uri")
    }

    return config.JwksURI, nil
}
```

#### Application Service

No application service changes. The JWT adapter is infrastructure (an adapter
implementing a port). The auth middleware handles the authentication flow.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/ports/jwks.go` | JWKSFetcher interface |
| `internal/adapters/jwt/adapter.go` | JWT IdentityProvider implementation |
| `internal/adapters/jwt/adapter_test.go` | Unit tests for JWT adapter |
| `internal/adapters/jwt/jwks_fetcher.go` | HTTP JWKS fetcher implementation |
| `internal/adapters/jwt/jwks_fetcher_test.go` | Unit tests for JWKS fetcher |
| `internal/adapters/jwt/discovery.go` | OIDC Discovery helpers |
| `internal/adapters/jwt/discovery_test.go` | Unit tests for discovery |
| `internal/adapters/jwt/integration_test.go` | Integration tests with mock OIDC server |
| `internal/domain/events/jwt.go` | JWT-specific event constructors |
| `internal/domain/events/jwt_test.go` | Event tests |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `JWTConfig` struct, update `AuthConfig` |
| `internal/config/config_test.go` | Tests for JWT config validation |
| `internal/middleware/identity_headers.go` | Support claims-to-headers mapping |
| `internal/middleware/identity_headers_test.go` | Tests for claims mapping |
| `internal/plugins/auth/plugin.go` | Create JWT adapter when configured |
| `go.mod` | Promote go-jose/v4 from indirect to direct |

#### Configuration Schema

**Update `internal/config/config.go`:**

```go
// JWTConfig holds JWT/OIDC authentication settings.
type JWTConfig struct {
    // JWKSURL is the URL to fetch the JSON Web Key Set.
    // Mutually exclusive with IssuerURL: if both are set, JWKSURL takes precedence.
    JWKSURL string `mapstructure:"jwks_url"`

    // IssuerURL is the OIDC issuer URL for auto-discovery.
    // When set, JWKS URL is discovered from /.well-known/openid-configuration.
    IssuerURL string `mapstructure:"issuer_url"`

    // Issuer is the expected "iss" claim value. Required when provider is "jwt".
    Issuer string `mapstructure:"issuer"`

    // Audience is the expected "aud" claim value. Required when provider is "jwt".
    Audience string `mapstructure:"audience"`

    // ClaimsToHeaders maps JWT claim names to HTTP header names.
    // The mapped claims are injected into requests forwarded to the upstream app.
    // Default: {"sub": "X-User-Id", "email": "X-User-Email", "email_verified": "X-User-Verified"}
    ClaimsToHeaders map[string]string `mapstructure:"claims_to_headers"`

    // AllowedAlgorithms restricts which signing algorithms are accepted.
    // Default: ["RS256", "ES256"].
    AllowedAlgorithms []string `mapstructure:"allowed_algorithms"`

    // CacheTTL is how long to cache the JWKS before refreshing.
    // Default: "1h".
    CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// AuthMode is an enum for the authentication strategy.
type AuthMode string

const (
    AuthModeKratos AuthMode = "kratos"
    AuthModeJWT    AuthMode = "jwt"
    AuthModeAPIKey AuthMode = "api-key"
    AuthModeNone   AuthMode = "none"
)

// AuthConfig updated:
type AuthConfig struct {
    Enabled           bool                    `mapstructure:"enabled"`
    Mode              AuthMode                `mapstructure:"mode"` // Updated: add "jwt"
    JWT               JWTConfig               `mapstructure:"jwt"`  // New field
    APIKey            AuthAPIKeyConfig        `mapstructure:"api_key"`
    // ... existing fields ...
}
```

**Example YAML configuration:**

```yaml
auth:
  enabled: true
  provider: jwt
  jwt:
    # Option 1: Direct JWKS URL
    jwks_url: https://example.auth0.com/.well-known/jwks.json

    # Option 2: OIDC Discovery (auto-detect JWKS URL)
    # issuer_url: https://example.auth0.com/

    issuer: https://example.auth0.com/
    audience: my-api

    claims_to_headers:
      sub: X-User-Id
      email: X-User-Email
      name: X-User-Name
      roles: X-User-Roles

    allowed_algorithms:
      - RS256
      - ES256

    cache_ttl: 1h
```

#### Sequence

**JWT authentication flow:**

1. HTTP request arrives at VibeWarden with `Authorization: Bearer <token>` header
2. Auth middleware calls `jwtAdapter.Authenticate(ctx, r)`
3. JWT adapter extracts Bearer token from Authorization header
4. If no token present, return `Failure("no_credentials", ...)`
5. Parse JWT (without verification) to extract key ID (kid) from header
6. Call `fetcher.GetKey(ctx, kid)` to retrieve the signing key
7. If JWKS fetch fails (network error), return `Failure("provider_unavailable", ...)`
8. Verify JWT signature using the key
9. If signature invalid, return `Failure("invalid_signature", ...)`
10. Validate standard claims (iss, aud, exp, iat, nbf)
11. If validation fails, return `Failure("token_expired" | "invalid_issuer" | ...)` 
12. Extract `sub`, `email`, custom claims and build `identity.Identity`
13. Return `Success(identity)`
14. Auth middleware stores Identity in request context
15. IdentityHeadersMiddleware injects configured claims as X-User-* headers
16. Request forwarded to upstream application

**JWKS caching flow:**

1. First request triggers JWKS fetch from `jwks_url`
2. Keys cached in memory with timestamp
3. Subsequent requests use cached keys if within `cache_ttl`
4. If `kid` not found in cache, trigger immediate refresh (handles key rotation)
5. Background refresh recommended but not required (lazy refresh on TTL expiry)

#### Error Cases

| Error | HTTP Status | Reason Code | Handling |
|-------|-------------|-------------|----------|
| No Authorization header | 302 Redirect | `no_credentials` | Redirect to login URL |
| Not a Bearer token | 302 Redirect | `no_credentials` | Redirect to login URL |
| JWT parse error | 401 | `invalid_token` | Return error response |
| Key ID not found | 401 | `invalid_signature` | Return error response |
| Signature verification failed | 401 | `invalid_signature` | Return error response |
| Token expired | 401 | `token_expired` | Return error response |
| Token not yet valid | 401 | `token_not_yet_valid` | Return error response |
| Invalid issuer | 401 | `invalid_issuer` | Return error response |
| Invalid audience | 401 | `invalid_audience` | Return error response |
| JWKS endpoint unreachable | 503 | `provider_unavailable` | Fail closed |
| JWKS timeout | 503 | `provider_unavailable` | Fail closed |

Note: For JWT mode, `no_credentials` may want to return 401 instead of redirect,
depending on use case (API vs. browser). Make this configurable via
`auth.jwt.on_missing_token: "redirect" | "401"` (default: "401" for JWT mode).

#### Structured Log Events

**New events in `internal/domain/events/jwt.go`:**

```go
const (
    EventTypeJWTValid    = "auth.jwt_valid"
    EventTypeJWTInvalid  = "auth.jwt_invalid"
    EventTypeJWTExpired  = "auth.jwt_expired"
    EventTypeJWKSRefresh = "auth.jwks_refresh"
    EventTypeJWKSError   = "auth.jwks_error"
)

// JWTValidParams contains parameters for auth.jwt_valid event.
type JWTValidParams struct {
    Method     string
    Path       string
    Subject    string
    Issuer     string
    Audience   string
}

// NewJWTValid creates an auth.jwt_valid event.
func NewJWTValid(params JWTValidParams) Event { ... }

// JWTInvalidParams contains parameters for auth.jwt_invalid event.
type JWTInvalidParams struct {
    Method string
    Path   string
    Reason string // "invalid_signature", "invalid_issuer", etc.
    Detail string
}

// NewJWTInvalid creates an auth.jwt_invalid event.
func NewJWTInvalid(params JWTInvalidParams) Event { ... }

// JWTExpiredParams contains parameters for auth.jwt_expired event.
type JWTExpiredParams struct {
    Method    string
    Path      string
    Subject   string
    ExpiredAt time.Time
}

// NewJWTExpired creates an auth.jwt_expired event.
func NewJWTExpired(params JWTExpiredParams) Event { ... }
```

#### Test Strategy

**Unit tests (`*_test.go` files):**

- `adapter_test.go`:
  - Test Authenticate with valid token (mock fetcher returns valid key)
  - Test Authenticate with missing Authorization header
  - Test Authenticate with non-Bearer Authorization
  - Test Authenticate with expired token
  - Test Authenticate with wrong issuer
  - Test Authenticate with wrong audience
  - Test Authenticate with invalid signature
  - Test Authenticate with unknown kid
  - Test claims extraction and Identity mapping
  - Test claims-to-headers mapping

- `jwks_fetcher_test.go`:
  - Test FetchKeys returns cached JWKS when within TTL
  - Test FetchKeys refreshes when TTL expired
  - Test GetKey finds existing key
  - Test GetKey triggers refresh on missing key
  - Test concurrent access to cache is safe
  - Test HTTP error handling (non-200, timeout)

- `discovery_test.go`:
  - Test DiscoverJWKSURL with valid OIDC config
  - Test DiscoverJWKSURL with missing jwks_uri
  - Test DiscoverJWKSURL with HTTP error

**Integration tests (`integration_test.go`):**

- Spin up httptest.Server serving mock JWKS
- Generate valid RS256 and ES256 tokens
- Test full flow: token -> adapter -> Identity
- Test key rotation: remove old key, add new key, verify refresh
- Test OIDC discovery against mock discovery endpoint

**Config validation tests:**

- Test that `auth.provider: jwt` requires `jwt.issuer` and `jwt.audience`
- Test that at least one of `jwks_url` or `issuer_url` is required
- Test default values for `allowed_algorithms` and `cache_ttl`

#### New Dependencies

**Direct dependency (promote from indirect):**

| Library | Version | License | Reason |
|---------|---------|---------|--------|
| `github.com/go-jose/go-jose/v4` | v4.1.3+ | Apache 2.0 | JWT parsing, signature verification, JWKS handling |

Already an indirect dependency. License verified: Apache 2.0.

**No new dependencies.** go-jose/v4 is the standard library for JOSE in Go,
maintained by the original authors of square/go-jose, and is already in the
dependency tree via Ory Kratos.

### Consequences

**Positive:**

- Users with existing OIDC providers (Auth0, Okta, Keycloak) can use VibeWarden
  without running Kratos
- Stateless authentication — no session storage needed
- Standard OIDC compatibility — works with any compliant provider
- JWKS caching minimizes network calls and latency
- Key rotation handled automatically via refresh-on-missing-key
- Claims-to-headers mapping gives upstream apps full access to user context

**Negative:**

- No session revocation — JWTs are valid until expiry (stateless trade-off)
- JWKS endpoint becomes a dependency — if unreachable, auth fails (fail-closed)
- More configuration surface than Kratos (issuer, audience, etc.)

**Trade-offs:**

- **Fail-closed on JWKS unavailable:** Chose security over availability. If we
  cannot verify signatures, we cannot trust tokens. Configure short JWKS cache
  TTL (1h default) with refresh-on-missing-key for resilience.

- **Lazy refresh vs. background refresh:** Chose lazy refresh (refresh on cache
  miss or TTL expiry) for simplicity. Background refresh adds complexity and a
  goroutine lifecycle to manage. Lazy refresh is sufficient for most workloads.

- **go-jose/v4 vs. lestrrat-go/jwx:** Chose go-jose because it is already an
  indirect dependency (via Ory Kratos), avoiding dependency duplication. Both
  libraries are well-maintained with compatible licenses.

- **JWT mode returns 401 by default (not redirect):** JWT mode is typically for
  APIs, not browsers. Redirecting to a login URL makes no sense for API clients.
  Made `on_missing_token` configurable for hybrid use cases.

**Security considerations:**

- Algorithm restriction: Only RS256 and ES256 by default. Never allow `none` or
  symmetric algorithms like HS256 (where the secret would need to be shared).
- Issuer and audience validation mandatory: Prevents token substitution attacks.
- Key ID (kid) required: JWTs without kid are rejected to prevent key confusion.

---
