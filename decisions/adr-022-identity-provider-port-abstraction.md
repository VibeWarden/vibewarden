# ADR-022: Identity Provider Port Abstraction

**Date**: 2026-03-28
**Issue**: #385
**Status**: Accepted

### Context

VibeWarden currently supports Ory Kratos as the sole identity provider for session-based
authentication. The `SessionChecker` interface in `internal/ports/auth.go` is tightly
coupled to Kratos concepts (session cookies, Kratos-specific Identity/Session types).

Epic #373 ("Flexible Auth") requires support for multiple identity providers:
- Ory Kratos (existing, session-based)
- JWT/OIDC validation (stateless tokens from external IdPs like Auth0, Okta, Keycloak)
- API keys (already implemented separately in ADR for API key auth)

The current `SessionChecker` interface cannot support JWT/OIDC because:
1. JWT validation uses `Authorization: Bearer` headers, not session cookies
2. JWT claims have a different structure than Kratos sessions
3. The `Identity` and `Session` types in ports are Kratos-specific
4. The auth middleware directly references Kratos concepts

**Acceptance criteria from issue #385:**
- Create a generic `IdentityProvider` port that abstracts session/token validation
- The port returns a provider-agnostic `Identity` value object
- Existing Kratos adapter implements the new port
- Auth middleware uses the new port instead of `SessionChecker`
- Backward compatibility: no changes to existing configuration or behavior

### Decision

Introduce a new `IdentityProvider` port interface that abstracts the authentication
mechanism (session cookie vs. Bearer token vs. API key). The existing `SessionChecker`
interface is preserved for backward compatibility but marked as deprecated.

#### Domain Model Changes

**New value object: `internal/domain/identity/identity.go`**

The `Identity` value object represents a verified user identity independent of the
authentication mechanism. It is immutable and has no behavior beyond validation.

```go
// Package identity provides domain types for authenticated user identity.
// This package has zero external dependencies — only the Go standard library.
package identity

import (
    "errors"
    "strings"
)

// Identity is a value object representing an authenticated user's identity.
// It is provider-agnostic: the same Identity type is returned whether the user
// authenticated via Kratos session, JWT, API key, or any future mechanism.
//
// Identity is immutable. Create new instances via NewIdentity.
type Identity struct {
    // id is the unique identifier for the user. Format depends on the provider:
    // - Kratos: UUID string
    // - OIDC: "sub" claim value
    // - API key: key name or hash
    id string

    // email is the user's primary email address. May be empty for non-human
    // identities (e.g., service accounts, API keys).
    email string

    // emailVerified indicates whether the email has been verified by the provider.
    emailVerified bool

    // provider identifies which identity provider authenticated this user.
    // Examples: "kratos", "oidc", "jwt", "apikey".
    provider string

    // claims contains additional attributes from the provider. Keys are
    // claim names, values are claim values (typically string, []string, or bool).
    // For Kratos: traits
    // For JWT/OIDC: all claims except reserved ones (sub, iss, aud, exp, iat, nbf)
    // For API keys: scopes as {"scopes": []string{...}}
    claims map[string]any
}

// NewIdentity creates a new Identity with the given attributes.
// Returns an error if required fields are invalid.
func NewIdentity(id, email, provider string, emailVerified bool, claims map[string]any) (Identity, error) {
    if id == "" {
        return Identity{}, errors.New("identity id cannot be empty")
    }
    if provider == "" {
        return Identity{}, errors.New("identity provider cannot be empty")
    }
    // Email validation: if provided, must contain @
    if email != "" && !strings.Contains(email, "@") {
        return Identity{}, errors.New("invalid email format")
    }

    // Defensive copy of claims to ensure immutability
    claimsCopy := make(map[string]any, len(claims))
    for k, v := range claims {
        claimsCopy[k] = v
    }

    return Identity{
        id:            id,
        email:         email,
        emailVerified: emailVerified,
        provider:      provider,
        claims:        claimsCopy,
    }, nil
}

// ID returns the user's unique identifier.
func (i Identity) ID() string { return i.id }

// Email returns the user's email address. May be empty.
func (i Identity) Email() string { return i.email }

// EmailVerified returns true if the email has been verified.
func (i Identity) EmailVerified() bool { return i.emailVerified }

// Provider returns the name of the identity provider that authenticated this user.
func (i Identity) Provider() string { return i.provider }

// Claims returns a copy of the additional claims map.
// Modifying the returned map does not affect the Identity.
func (i Identity) Claims() map[string]any {
    copy := make(map[string]any, len(i.claims))
    for k, v := range i.claims {
        copy[k] = v
    }
    return copy
}

// Claim returns the value of a specific claim, or nil if not present.
func (i Identity) Claim(name string) any {
    return i.claims[name]
}

// HasClaim reports whether the identity has the named claim.
func (i Identity) HasClaim(name string) bool {
    _, ok := i.claims[name]
    return ok
}

// IsZero reports whether this is the zero value (no identity).
func (i Identity) IsZero() bool {
    return i.id == ""
}
```

**New value object: `internal/domain/identity/auth_result.go`**

```go
package identity

// AuthResult represents the outcome of an authentication attempt.
// It contains either a valid Identity or information about why auth failed.
type AuthResult struct {
    // Identity is the authenticated user's identity. Zero value if auth failed.
    Identity Identity

    // Authenticated is true if authentication succeeded.
    Authenticated bool

    // Reason is a machine-readable code explaining auth failure (e.g., "token_expired",
    // "invalid_signature", "session_not_found"). Empty when Authenticated is true.
    Reason string

    // Message is a human-readable description of the failure. Empty when Authenticated is true.
    Message string
}

// Success creates an AuthResult for a successful authentication.
func Success(identity Identity) AuthResult {
    return AuthResult{
        Identity:      identity,
        Authenticated: true,
    }
}

// Failure creates an AuthResult for a failed authentication.
func Failure(reason, message string) AuthResult {
    return AuthResult{
        Authenticated: false,
        Reason:        reason,
        Message:       message,
    }
}
```

#### Ports (Interfaces)

**New file: `internal/ports/identity.go`**

```go
package ports

import (
    "context"
    "net/http"

    "github.com/vibewarden/vibewarden/internal/domain/identity"
)

// IdentityProvider validates authentication credentials from an HTTP request
// and returns the authenticated user's identity.
//
// This is the primary authentication port. Implementations include:
// - Kratos adapter (session cookie validation)
// - JWT adapter (Bearer token validation)
// - API key adapter (X-API-Key header validation)
//
// The auth middleware chains multiple IdentityProviders when configured,
// trying each in order until one succeeds or all fail.
type IdentityProvider interface {
    // Name returns the provider identifier (e.g., "kratos", "jwt", "apikey").
    // Used for logging, metrics labels, and the Identity.Provider field.
    Name() string

    // Authenticate extracts credentials from the request and validates them.
    // Returns an AuthResult indicating success or failure.
    //
    // If the provider cannot find any credentials it recognizes (e.g., no session
    // cookie for Kratos, no Bearer token for JWT), it returns a Failure result
    // with Reason "no_credentials". This allows the middleware to try the next
    // provider in the chain.
    //
    // If credentials are present but invalid, it returns a Failure result with
    // a specific Reason (e.g., "token_expired", "session_invalid").
    //
    // The context may carry request-scoped values (trace context, etc.).
    // Implementations must honour context cancellation.
    Authenticate(ctx context.Context, r *http.Request) identity.AuthResult
}

// IdentityProviderUnavailable is returned when the underlying identity service
// (e.g., Kratos, JWKS endpoint) cannot be reached. Middleware should handle this
// according to the configured degradation mode (fail-closed vs. allow-public).
type IdentityProviderUnavailable struct {
    Provider string
    Cause    error
}

func (e IdentityProviderUnavailable) Error() string {
    return "identity provider " + e.Provider + " unavailable: " + e.Cause.Error()
}

func (e IdentityProviderUnavailable) Unwrap() error {
    return e.Cause
}
```

**Update: `internal/ports/auth.go`**

Mark `SessionChecker` as deprecated but preserve for backward compatibility:

```go
// SessionChecker validates sessions against an identity provider.
//
// Deprecated: Use IdentityProvider instead. SessionChecker will be removed in v2.
// The Kratos adapter implements both interfaces during the migration period.
type SessionChecker interface {
    // CheckSession validates the given session cookie and returns the session if valid.
    // Returns ErrSessionInvalid if the session is invalid or expired.
    // Returns ErrSessionNotFound if no session exists for the cookie.
    // Returns ErrAuthProviderUnavailable when the identity provider cannot be reached.
    CheckSession(ctx context.Context, sessionCookie string) (*Session, error)
}
```

#### Adapters

**Update: `internal/adapters/kratos/adapter.go`**

The Kratos adapter now implements both `SessionChecker` (deprecated) and `IdentityProvider`.

```go
package kratos

import (
    // ... existing imports ...
    "github.com/vibewarden/vibewarden/internal/domain/identity"
)

// Adapter implements ports.SessionChecker and ports.IdentityProvider
// using the Ory Kratos public API.
type Adapter struct {
    publicURL     string
    client        *http.Client
    logger        *slog.Logger
    cookieName    string
}

// NewAdapter creates a new Kratos adapter.
// publicURL is the base URL of the Kratos public API (e.g. "http://localhost:4433").
// cookieName is the session cookie name (default: "ory_kratos_session").
func NewAdapter(publicURL string, cookieName string, timeout time.Duration, logger *slog.Logger) *Adapter {
    if timeout == 0 {
        timeout = defaultTimeout
    }
    if cookieName == "" {
        cookieName = defaultCookieName
    }
    return &Adapter{
        publicURL:  publicURL,
        client:     &http.Client{Timeout: timeout},
        logger:     logger,
        cookieName: cookieName,
    }
}

// Name implements ports.IdentityProvider.
func (a *Adapter) Name() string { return "kratos" }

// Authenticate implements ports.IdentityProvider.
// It extracts the session cookie from the request, validates it with Kratos,
// and returns an AuthResult with the user's identity.
func (a *Adapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
    // Extract session cookie
    cookie, err := r.Cookie(a.cookieName)
    if err != nil {
        // No cookie = no credentials for this provider
        return identity.Failure("no_credentials", "no session cookie")
    }

    sessionCookie := a.cookieName + "=" + cookie.Value

    // Validate with Kratos
    session, err := a.CheckSession(ctx, sessionCookie)
    if err != nil {
        switch {
        case errors.Is(err, ports.ErrSessionInvalid):
            return identity.Failure("session_invalid", "session is invalid or expired")
        case errors.Is(err, ports.ErrSessionNotFound):
            return identity.Failure("session_not_found", "session does not exist")
        case errors.Is(err, ports.ErrAuthProviderUnavailable):
            return identity.Failure("provider_unavailable", err.Error())
        default:
            return identity.Failure("auth_error", err.Error())
        }
    }

    // Map Kratos session to domain Identity
    ident, err := identity.NewIdentity(
        session.Identity.ID,
        session.Identity.Email,
        "kratos",
        session.Identity.EmailVerified,
        session.Identity.Traits,
    )
    if err != nil {
        return identity.Failure("invalid_identity", err.Error())
    }

    return identity.Success(ident)
}

// CheckSession implements ports.SessionChecker (deprecated).
// Retained for backward compatibility; new code should use Authenticate.
func (a *Adapter) CheckSession(ctx context.Context, sessionCookie string) (*ports.Session, error) {
    // ... existing implementation unchanged ...
}
```

#### Application Service

No application service changes. The auth middleware is infrastructure, not a use case.

#### File Layout

**New files:**

| File | Purpose |
|------|---------|
| `internal/domain/identity/identity.go` | Identity value object |
| `internal/domain/identity/identity_test.go` | Unit tests for Identity |
| `internal/domain/identity/auth_result.go` | AuthResult value object |
| `internal/domain/identity/auth_result_test.go` | Unit tests for AuthResult |
| `internal/ports/identity.go` | IdentityProvider interface |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/ports/auth.go` | Add deprecation notice to SessionChecker |
| `internal/adapters/kratos/adapter.go` | Add Name() and Authenticate() methods |
| `internal/adapters/kratos/adapter_test.go` | Add tests for IdentityProvider implementation |
| `internal/middleware/auth.go` | Accept IdentityProvider, fall back to SessionChecker wrapper |
| `internal/middleware/auth_test.go` | Update tests for IdentityProvider |
| `internal/plugins/auth/plugin.go` | Create IdentityProvider adapter, pass to middleware |

#### Sequence

**Authentication flow with new IdentityProvider:**

1. HTTP request arrives at VibeWarden
2. Auth middleware calls `provider.Authenticate(ctx, r)`
3. Kratos adapter:
   a. Extracts session cookie from request
   b. If no cookie, returns `Failure("no_credentials", ...)`
   c. Calls Kratos `/sessions/whoami` with cookie
   d. If Kratos returns 401, returns `Failure("session_invalid", ...)`
   e. If Kratos unavailable, returns `Failure("provider_unavailable", ...)`
   f. Parses response, creates domain `Identity` value object
   g. Returns `Success(identity)`
4. Auth middleware:
   a. If `Authenticated == true`: stores Identity in context, calls next handler
   b. If `Reason == "no_credentials"`: tries next provider (if chained) or redirects to login
   c. If `Reason == "provider_unavailable"`: handles per degradation mode config
   d. If other failure: redirects to login or returns error

**Backward compatibility flow:**

When code still uses the deprecated `SessionChecker`:
1. Auth plugin detects old-style config (no `provider` field)
2. Creates Kratos adapter implementing both interfaces
3. Middleware receives `IdentityProvider`, uses new flow
4. Old `CheckSession` method is only used by legacy code paths

#### Error Cases

| Error | Cause | Handling |
|-------|-------|----------|
| No credentials | Cookie/token absent | Return Failure("no_credentials"), try next provider |
| Session invalid | Expired or revoked | Return Failure("session_invalid"), redirect to login |
| Provider unavailable | Network/timeout | Return Failure("provider_unavailable"), use degradation mode |
| Invalid identity data | Malformed Kratos response | Return Failure("invalid_identity"), log error |

#### Test Strategy

**Unit tests:**

| File | Coverage |
|------|----------|
| `internal/domain/identity/identity_test.go` | NewIdentity validation, immutability, accessors |
| `internal/domain/identity/auth_result_test.go` | Success/Failure constructors |
| `internal/adapters/kratos/adapter_test.go` | Authenticate method with mock HTTP responses |
| `internal/middleware/auth_test.go` | Middleware with mock IdentityProvider |

**Unit test approach:**

- Test Identity value object in isolation (pure Go, no mocks needed)
- Test Kratos adapter with httptest server returning canned JSON
- Test middleware with fake IdentityProvider returning fixed AuthResults
- Table-driven tests for various auth scenarios

**Integration tests:**

| File | Coverage |
|------|----------|
| `internal/adapters/kratos/adapter_integration_test.go` | Real Kratos container via testcontainers |

**What to mock vs. real:**

- Mock: HTTP responses for unit tests
- Real: Kratos container for integration tests (existing setup)
- Real: Domain value objects (no external deps)

#### New Dependencies

**None.** This ADR introduces no new external dependencies. All new code uses:
- Go standard library
- Existing internal packages

### Consequences

**Positive:**

- Clean abstraction for multiple identity providers (Kratos, JWT, API keys)
- Domain-centric Identity type with no external dependencies
- Existing Kratos integration works unchanged
- Auth middleware becomes provider-agnostic
- Future JWT/OIDC adapter slots in cleanly
- Immutable value objects prevent accidental mutation

**Negative:**

- Two parallel interfaces during migration (SessionChecker deprecated)
- Slightly more code in Kratos adapter (implements both interfaces)
- Identity in domain layer vs. ports.Identity — minor type duplication

**Trade-offs:**

- **Identity in domain vs. ports:** Chose domain layer because Identity is a
  core concept with validation logic, not just a data transfer object. The
  ports layer will have a slim reference to domain.Identity rather than
  defining its own type.

- **AuthResult vs. error returns:** Chose explicit AuthResult value object over
  error returns because authentication failure is an expected outcome, not an
  exceptional condition. This makes the success/failure branches explicit and
  carries structured failure information.

- **Preserve SessionChecker vs. remove:** Chose deprecation over removal for
  backward compatibility. Any code using SessionChecker continues to work.
  Removal scheduled for v2.

- **Single Authenticate method vs. ExtractCredentials + Validate:** Chose
  single method for simplicity. The provider knows best how to extract its
  credentials; splitting adds complexity without benefit.

**Migration path:**

1. This story: Add IdentityProvider port, Kratos implements it, middleware updated
2. Future story #386: Add JWT/OIDC adapter implementing IdentityProvider
3. Future story #387: Add config to select/chain providers
4. v2: Remove deprecated SessionChecker interface
