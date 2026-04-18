# ADR-004: Request Routing Architecture (Caddy Embedding)


**Status:** Accepted
**Issue:** #2
**Date:** 2026-03-20

### Context

VibeWarden needs to proxy HTTP/HTTPS traffic from clients to the upstream application. The locked decision L-02 mandates embedding Caddy as a library (not subprocess). This epic delivers the core reverse proxy functionality including:

- Caddy embedded with programmatic JSON config
- Reverse proxying to upstream application
- Automatic TLS via Let's Encrypt (for non-localhost domains)
- Security headers middleware
- Health check endpoint

This is a large epic with several distinct components. To enable focused reviews and incremental delivery, it will be split into sub-issues.

### Decision

#### Epic Split Strategy

Split Epic #2 into four focused sub-issues:

| Sub-Issue | Title | Dependencies |
|-----------|-------|--------------|
| #2.1 | Core Caddy Embedding and Reverse Proxy | Epic #1 |
| #2.2 | Security Headers Middleware | #2.1 |
| #2.3 | Health Check Endpoint | #2.1 |
| #2.4 | TLS Automation (Let's Encrypt) | #2.1 |

Each sub-issue is independently testable and deployable.

#### Dependencies (License Verified)

| Dependency | Version | License | Purpose |
|------------|---------|---------|---------|
| github.com/caddyserver/caddy/v2 | latest (v2.10+) | Apache 2.0 | Reverse proxy engine (locked decision L-02) |

Caddy v2 brings in many transitive dependencies, but Caddy itself is Apache 2.0 licensed.

#### Architecture Overview

```
                    ┌─────────────────────────────────────────────────────┐
                    │                    VibeWarden                       │
                    │                                                     │
 Incoming ──────────┼──► ┌─────────────────┐    ┌───────────────────┐    │
 HTTP(S)            │    │  Caddy Adapter  │───►│ Upstream App      │    │
 Requests           │    │  (implements    │    │ (localhost:3000)  │    │
                    │    │   ProxyServer)  │◄───│                   │    │
                    │    └────────┬────────┘    └───────────────────┘    │
                    │             │                                       │
                    │    ┌────────▼────────┐                              │
                    │    │  Middleware     │                              │
                    │    │  Chain:         │                              │
                    │    │  - Security Hdr │                              │
                    │    │  - Health Check │                              │
                    │    │  - (future:auth)│                              │
                    │    │  - (future:rate)│                              │
                    │    └─────────────────┘                              │
                    └─────────────────────────────────────────────────────┘
```

#### Hexagonal Architecture Mapping

**Domain Layer** (`internal/domain/`)
- No domain entities in this epic (pure infrastructure)
- Value objects for configuration validation will be added if needed

**Ports Layer** (`internal/ports/`)
- `ProxyServer` interface - abstraction for the reverse proxy
- `Middleware` interface - abstraction for HTTP middleware chain

**Adapters Layer** (`internal/adapters/caddy/`)
- `CaddyAdapter` - implements `ProxyServer` using embedded Caddy
- Builds Caddy JSON config programmatically
- Manages Caddy lifecycle (start, stop, reload)

**Application Layer** (`internal/app/`)
- `ProxyService` - orchestrates proxy startup and middleware registration

### File Layout

New/modified files for this epic:

```
internal/
├── ports/
│   ├── proxy.go              # ProxyServer interface
│   └── middleware.go         # Middleware interface
├── adapters/
│   └── caddy/
│       ├── adapter.go        # CaddyAdapter implementation
│       ├── adapter_test.go   # Unit tests
│       ├── config.go         # Caddy JSON config builder
│       ├── config_test.go    # Config builder tests
│       └── middleware.go     # Caddy middleware integration
├── app/
│   └── proxy/
│       ├── service.go        # ProxyService
│       └── service_test.go   # Unit tests
├── middleware/
│   ├── security_headers.go   # Security headers middleware
│   ├── security_headers_test.go
│   ├── health.go             # Health check handler
│   └── health_test.go
└── config/
    └── config.go             # (existing - extend with security_headers config)
```

### Interface Definitions

#### ProxyServer Port (`internal/ports/proxy.go`)

```go
// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "context"

// ProxyServer defines the interface for the reverse proxy server.
// Implementations handle incoming HTTP(S) requests and forward them to upstream.
type ProxyServer interface {
    // Start begins listening for incoming requests.
    // Blocks until the context is cancelled or an error occurs.
    Start(ctx context.Context) error

    // Stop gracefully shuts down the proxy server.
    // The provided context controls the shutdown timeout.
    Stop(ctx context.Context) error

    // Reload applies configuration changes without dropping connections.
    // Not all implementations may support reload; they should return an error if not.
    Reload(ctx context.Context) error
}

// ProxyConfig holds configuration for the proxy server.
// This is a domain-agnostic configuration that ports can depend on.
type ProxyConfig struct {
    // ListenAddr is the address to bind to (e.g., "127.0.0.1:8080")
    ListenAddr string

    // UpstreamAddr is the address of the upstream application (e.g., "127.0.0.1:3000")
    UpstreamAddr string

    // TLS configuration
    TLS TLSConfig

    // SecurityHeaders configuration
    SecurityHeaders SecurityHeadersConfig
}

// TLSConfig holds TLS-specific settings.
type TLSConfig struct {
    // Enabled toggles TLS termination
    Enabled bool

    // Domain for certificate provisioning (required if Enabled && AutoCert)
    Domain string

    // AutoCert enables automatic certificate provisioning via ACME (Let's Encrypt)
    AutoCert bool

    // CertPath is the path to a custom certificate file (if not using AutoCert)
    CertPath string

    // KeyPath is the path to the private key file (if not using AutoCert)
    KeyPath string

    // StoragePath is where Caddy stores certificates (default: system-specific)
    StoragePath string
}

// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
    // Enabled toggles security headers middleware
    Enabled bool

    // HSTS settings
    HSTSMaxAge            int  // max-age in seconds (default: 31536000 = 1 year)
    HSTSIncludeSubDomains bool // includeSubDomains directive
    HSTSPreload           bool // preload directive

    // Content-Type-Options
    ContentTypeNosniff bool // X-Content-Type-Options: nosniff

    // Frame options
    FrameOption string // X-Frame-Options value: "DENY", "SAMEORIGIN", or "" (disabled)

    // CSP
    ContentSecurityPolicy string // Content-Security-Policy value (empty = disabled)

    // Referrer Policy
    ReferrerPolicy string // Referrer-Policy value (empty = disabled)

    // Permissions Policy (formerly Feature-Policy)
    PermissionsPolicy string // Permissions-Policy value (empty = disabled)
}
```

#### Middleware Port (`internal/ports/middleware.go`)

```go
package ports

import "net/http"

// Middleware defines an HTTP middleware that can be applied to requests.
// Middleware wraps an http.Handler and returns a new http.Handler.
type Middleware func(next http.Handler) http.Handler

// MiddlewareChain is an ordered list of middleware to apply.
type MiddlewareChain []Middleware

// Apply wraps the given handler with all middleware in the chain.
// Middleware is applied in reverse order so the first middleware in the chain
// is the outermost wrapper (executes first on request, last on response).
func (c MiddlewareChain) Apply(h http.Handler) http.Handler {
    for i := len(c) - 1; i >= 0; i-- {
        h = c[i](h)
    }
    return h
}
```

#### CaddyAdapter (`internal/adapters/caddy/adapter.go`)

```go
// Package caddy implements the ProxyServer port using embedded Caddy.
package caddy

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"

    "github.com/caddyserver/caddy/v2"
    "github.com/caddyserver/caddy/v2/caddyconfig"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// Adapter implements ports.ProxyServer using embedded Caddy.
type Adapter struct {
    config *ports.ProxyConfig
    logger *slog.Logger
}

// NewAdapter creates a new Caddy adapter with the given configuration.
func NewAdapter(cfg *ports.ProxyConfig, logger *slog.Logger) *Adapter {
    return &Adapter{
        config: cfg,
        logger: logger,
    }
}

// Start implements ports.ProxyServer.Start.
// It builds Caddy JSON configuration and runs Caddy.
func (a *Adapter) Start(ctx context.Context) error {
    cfg, err := a.buildConfig()
    if err != nil {
        return fmt.Errorf("building caddy config: %w", err)
    }

    cfgJSON, err := json.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("marshaling caddy config: %w", err)
    }

    a.logger.Info("starting caddy proxy",
        slog.String("listen", a.config.ListenAddr),
        slog.String("upstream", a.config.UpstreamAddr),
    )

    // Load and run Caddy with the configuration
    err = caddy.Load(cfgJSON, true)
    if err != nil {
        return fmt.Errorf("loading caddy config: %w", err)
    }

    // Wait for context cancellation
    <-ctx.Done()

    return nil
}

// Stop implements ports.ProxyServer.Stop.
func (a *Adapter) Stop(ctx context.Context) error {
    a.logger.Info("stopping caddy proxy")
    return caddy.Stop()
}

// Reload implements ports.ProxyServer.Reload.
func (a *Adapter) Reload(ctx context.Context) error {
    cfg, err := a.buildConfig()
    if err != nil {
        return fmt.Errorf("building caddy config: %w", err)
    }

    cfgJSON, err := json.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("marshaling caddy config: %w", err)
    }

    a.logger.Info("reloading caddy configuration")

    return caddy.Load(cfgJSON, true)
}

// buildConfig constructs the Caddy JSON configuration.
func (a *Adapter) buildConfig() (map[string]any, error) {
    // Implementation in config.go
    return BuildCaddyConfig(a.config)
}
```

#### Caddy Config Builder (`internal/adapters/caddy/config.go`)

```go
package caddy

import (
    "fmt"
    "net"
    "strings"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// BuildCaddyConfig constructs the Caddy JSON configuration from ProxyConfig.
// Uses Caddy's native JSON config format (not Caddyfile).
func BuildCaddyConfig(cfg *ports.ProxyConfig) (map[string]any, error) {
    // Determine if this is a local address (skip TLS for localhost)
    isLocal := isLocalAddress(cfg.UpstreamAddr) || isLocalAddress(cfg.ListenAddr)

    // Build the reverse proxy handler
    reverseProxyHandler := map[string]any{
        "handler": "reverse_proxy",
        "upstreams": []map[string]any{
            {"dial": cfg.UpstreamAddr},
        },
    }

    // Build route handlers (middleware chain + reverse proxy)
    handlers := []map[string]any{}

    // Add security headers handler if enabled
    if cfg.SecurityHeaders.Enabled {
        handlers = append(handlers, buildSecurityHeadersHandler(cfg.SecurityHeaders))
    }

    // Add reverse proxy as final handler
    handlers = append(handlers, reverseProxyHandler)

    // Build routes
    routes := []map[string]any{
        {
            "handle": handlers,
        },
    }

    // Build the server configuration
    server := map[string]any{
        "listen": []string{cfg.ListenAddr},
        "routes": routes,
    }

    // Configure TLS if enabled and not local
    if cfg.TLS.Enabled && !isLocal {
        server["tls_connection_policies"] = buildTLSPolicy(cfg.TLS)

        // Add automatic HTTPS redirect
        server["automatic_https"] = map[string]any{
            "disable": false,
        }
    } else {
        // Disable automatic HTTPS for local development
        server["automatic_https"] = map[string]any{
            "disable": true,
        }
    }

    // Build apps configuration
    apps := map[string]any{
        "http": map[string]any{
            "servers": map[string]any{
                "vibewarden": server,
            },
        },
    }

    // Configure TLS automation if enabled
    if cfg.TLS.Enabled && cfg.TLS.AutoCert && !isLocal {
        apps["tls"] = buildTLSAutomation(cfg.TLS)
    }

    return map[string]any{
        "apps": apps,
    }, nil
}

// buildSecurityHeadersHandler creates the Caddy headers handler for security headers.
func buildSecurityHeadersHandler(cfg ports.SecurityHeadersConfig) map[string]any {
    headers := map[string][]string{}

    // HSTS
    if cfg.HSTSMaxAge > 0 {
        hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
        if cfg.HSTSIncludeSubDomains {
            hsts += "; includeSubDomains"
        }
        if cfg.HSTSPreload {
            hsts += "; preload"
        }
        headers["Strict-Transport-Security"] = []string{hsts}
    }

    // X-Content-Type-Options
    if cfg.ContentTypeNosniff {
        headers["X-Content-Type-Options"] = []string{"nosniff"}
    }

    // X-Frame-Options
    if cfg.FrameOption != "" {
        headers["X-Frame-Options"] = []string{cfg.FrameOption}
    }

    // Content-Security-Policy
    if cfg.ContentSecurityPolicy != "" {
        headers["Content-Security-Policy"] = []string{cfg.ContentSecurityPolicy}
    }

    // Referrer-Policy
    if cfg.ReferrerPolicy != "" {
        headers["Referrer-Policy"] = []string{cfg.ReferrerPolicy}
    }

    // Permissions-Policy
    if cfg.PermissionsPolicy != "" {
        headers["Permissions-Policy"] = []string{cfg.PermissionsPolicy}
    }

    return map[string]any{
        "handler": "headers",
        "response": map[string]any{
            "set": headers,
        },
    }
}

// buildTLSPolicy creates TLS connection policies for Caddy.
func buildTLSPolicy(cfg ports.TLSConfig) []map[string]any {
    return []map[string]any{
        {
            // Default policy - Caddy handles the rest
        },
    }
}

// buildTLSAutomation configures automatic certificate management.
func buildTLSAutomation(cfg ports.TLSConfig) map[string]any {
    automation := map[string]any{
        "automation": map[string]any{
            "policies": []map[string]any{
                {
                    "subjects": []string{cfg.Domain},
                    "issuers": []map[string]any{
                        {
                            "module": "acme",
                        },
                    },
                },
            },
        },
    }

    // Configure certificate storage if specified
    if cfg.StoragePath != "" {
        automation["storage"] = map[string]any{
            "module": "file_system",
            "root":   cfg.StoragePath,
        }
    }

    return automation
}

// isLocalAddress checks if the address is localhost or a loopback address.
func isLocalAddress(addr string) bool {
    host, _, err := net.SplitHostPort(addr)
    if err != nil {
        host = addr
    }

    host = strings.ToLower(host)

    if host == "localhost" || host == "" {
        return true
    }

    ip := net.ParseIP(host)
    if ip == nil {
        return false
    }

    return ip.IsLoopback()
}
```

#### Health Check Handler (`internal/middleware/health.go`)

```go
// Package middleware provides HTTP middleware for VibeWarden.
package middleware

import (
    "encoding/json"
    "net/http"
)

// HealthResponse is the JSON response from the health endpoint.
type HealthResponse struct {
    Status  string `json:"status"`
    Version string `json:"version"`
}

// HealthHandler returns an http.HandlerFunc for the health check endpoint.
// The health endpoint is served at /_vibewarden/health.
func HealthHandler(version string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        resp := HealthResponse{
            Status:  "ok",
            Version: version,
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)

        _ = json.NewEncoder(w).Encode(resp)
    }
}

// HealthMiddleware intercepts requests to /_vibewarden/health and serves
// the health response. All other requests pass through to the next handler.
func HealthMiddleware(version string) func(next http.Handler) http.Handler {
    healthHandler := HealthHandler(version)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.URL.Path == "/_vibewarden/health" {
                healthHandler(w, r)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

#### Security Headers Middleware (`internal/middleware/security_headers.go`)

```go
package middleware

import (
    "fmt"
    "net/http"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// SecurityHeaders creates a middleware that adds security headers to responses.
// This middleware applies headers after the response from upstream but before
// sending to the client.
func SecurityHeaders(cfg ports.SecurityHeadersConfig) func(next http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Set security headers before calling next handler
            setSecurityHeaders(w, cfg)

            next.ServeHTTP(w, r)
        })
    }
}

// setSecurityHeaders applies all configured security headers to the response.
func setSecurityHeaders(w http.ResponseWriter, cfg ports.SecurityHeadersConfig) {
    // HSTS
    if cfg.HSTSMaxAge > 0 {
        hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
        if cfg.HSTSIncludeSubDomains {
            hsts += "; includeSubDomains"
        }
        if cfg.HSTSPreload {
            hsts += "; preload"
        }
        w.Header().Set("Strict-Transport-Security", hsts)
    }

    // X-Content-Type-Options
    if cfg.ContentTypeNosniff {
        w.Header().Set("X-Content-Type-Options", "nosniff")
    }

    // X-Frame-Options
    if cfg.FrameOption != "" {
        w.Header().Set("X-Frame-Options", cfg.FrameOption)
    }

    // Content-Security-Policy
    if cfg.ContentSecurityPolicy != "" {
        w.Header().Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
    }

    // Referrer-Policy
    if cfg.ReferrerPolicy != "" {
        w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
    }

    // Permissions-Policy
    if cfg.PermissionsPolicy != "" {
        w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
    }
}

// DefaultSecurityHeadersConfig returns sensible default security header settings.
func DefaultSecurityHeadersConfig() ports.SecurityHeadersConfig {
    return ports.SecurityHeadersConfig{
        Enabled:               true,
        HSTSMaxAge:            31536000, // 1 year
        HSTSIncludeSubDomains: true,
        HSTSPreload:           false, // Preload requires manual submission
        ContentTypeNosniff:    true,
        FrameOption:           "DENY",
        ContentSecurityPolicy: "default-src 'self'",
        ReferrerPolicy:        "strict-origin-when-cross-origin",
        PermissionsPolicy:     "",
    }
}
```

#### ProxyService (`internal/app/proxy/service.go`)

```go
// Package proxy provides the application service for the reverse proxy.
package proxy

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates the reverse proxy lifecycle.
type Service struct {
    server ports.ProxyServer
    logger *slog.Logger
}

// NewService creates a new proxy service with the given server implementation.
func NewService(server ports.ProxyServer, logger *slog.Logger) *Service {
    return &Service{
        server: server,
        logger: logger,
    }
}

// Run starts the proxy server and blocks until the context is cancelled.
// On context cancellation, it initiates graceful shutdown.
func (s *Service) Run(ctx context.Context) error {
    // Create a child context for the server
    serverCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    // Channel to receive server errors
    errCh := make(chan error, 1)

    // Start server in goroutine
    go func() {
        errCh <- s.server.Start(serverCtx)
    }()

    // Wait for context cancellation or server error
    select {
    case <-ctx.Done():
        s.logger.Info("shutdown signal received, stopping proxy")
        // Context for graceful shutdown
        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer shutdownCancel()

        if err := s.server.Stop(shutdownCtx); err != nil {
            return fmt.Errorf("stopping proxy: %w", err)
        }
        return ctx.Err()

    case err := <-errCh:
        if err != nil {
            return fmt.Errorf("proxy server error: %w", err)
        }
        return nil
    }
}

// Reload reloads the proxy configuration without dropping connections.
func (s *Service) Reload(ctx context.Context) error {
    s.logger.Info("reloading proxy configuration")
    return s.server.Reload(ctx)
}
```

### Config Extension

Add to `internal/config/config.go`:

```go
// SecurityHeadersConfig holds security header settings.
type SecurityHeadersConfig struct {
    // Enabled toggles security headers (default: true)
    Enabled bool `mapstructure:"enabled"`

    // HSTS max-age in seconds (default: 31536000 = 1 year)
    HSTSMaxAge int `mapstructure:"hsts_max_age"`
    // Include subdomains in HSTS (default: true)
    HSTSIncludeSubDomains bool `mapstructure:"hsts_include_subdomains"`
    // HSTS preload (default: false - requires manual submission)
    HSTSPreload bool `mapstructure:"hsts_preload"`

    // X-Content-Type-Options: nosniff (default: true)
    ContentTypeNosniff bool `mapstructure:"content_type_nosniff"`

    // X-Frame-Options value: "DENY", "SAMEORIGIN", or "" to disable (default: "DENY")
    FrameOption string `mapstructure:"frame_option"`

    // Content-Security-Policy value (default: "default-src 'self'")
    ContentSecurityPolicy string `mapstructure:"content_security_policy"`

    // Referrer-Policy value (default: "strict-origin-when-cross-origin")
    ReferrerPolicy string `mapstructure:"referrer_policy"`

    // Permissions-Policy value (default: "")
    PermissionsPolicy string `mapstructure:"permissions_policy"`
}
```

And add to the Config struct:
```go
// Security headers configuration
SecurityHeaders SecurityHeadersConfig `mapstructure:"security_headers"`
```

### Request/Response Sequence

#### Proxy Request Flow (HTTP, no TLS)

1. Client sends HTTP request to `127.0.0.1:8080`
2. Caddy accepts connection on listen address
3. Health check middleware checks path:
   - If `/_vibewarden/health`: respond with JSON health status, return
   - Otherwise: continue chain
4. Security headers middleware adds headers to response writer
5. Reverse proxy handler forwards request to upstream (`127.0.0.1:3000`)
6. Upstream responds
7. Caddy forwards response (with security headers) to client

#### Proxy Request Flow (HTTPS with Let's Encrypt)

1. Client sends HTTPS request to `example.com:443`
2. Caddy performs TLS handshake (certificate from Let's Encrypt cache)
3. If certificate missing/expired: Caddy obtains new certificate via ACME
4. After TLS established, same flow as HTTP (steps 3-7)
5. HSTS header tells browser to use HTTPS in future

### Error Cases

| Error | Handling |
|-------|----------|
| Upstream unreachable | Caddy returns 502 Bad Gateway |
| TLS certificate failure | Caddy logs error, falls back to HTTP if possible |
| Invalid config | Return error from `Start()`, do not start server |
| Listen port in use | Return error from `Start()` with clear message |
| Graceful shutdown timeout | Force close connections after timeout |

### Test Strategy

**Unit Tests:**
- `internal/adapters/caddy/config_test.go` - Test JSON config generation for various scenarios
- `internal/middleware/security_headers_test.go` - Test header values for all configurations
- `internal/middleware/health_test.go` - Test health endpoint response format
- `internal/app/proxy/service_test.go` - Test lifecycle with mock ProxyServer

**Integration Tests:**
- `internal/adapters/caddy/adapter_integration_test.go`:
  - Start real Caddy with mock upstream (httptest.Server)
  - Make requests through proxy, verify they reach upstream
  - Verify security headers present in response
  - Verify health endpoint responds correctly
  - Test graceful shutdown

Integration tests will use build tag `//go:build integration` and can be run with `go test -tags=integration`.

### Consequences

**Positive:**
- Programmatic Caddy config enables dynamic configuration changes
- Caddy handles TLS complexity (ACME, renewal, OCSP stapling)
- Security headers are a single toggle with sensible defaults
- Health endpoint provides standard observability primitive
- Hexagonal architecture allows swapping Caddy for another proxy if needed

**Negative:**
- Caddy brings significant binary size increase (~15-20MB)
- Caddy's JSON config is verbose compared to Caddyfile
- Some Caddy modules may pull in unwanted dependencies

**Trade-offs:**
- Using Caddy's native handlers for security headers (via JSON config) vs custom Go middleware
  - Decision: Use both - Caddy handlers for production, Go middleware for tests
- Health endpoint under `/_vibewarden/` namespace to avoid conflicts

**Follow-up work:**
- Epic #3 (Auth) will add Kratos middleware to the chain
- Epic #4 (Rate Limiting) will add rate limiter middleware
- Epic #5 (Structured Logs) will add `proxy.started` event emission
- Epic #6 (CLI) will add full `serve` subcommand with config loading

---
