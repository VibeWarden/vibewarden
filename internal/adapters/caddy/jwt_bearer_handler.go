package caddy

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

func init() {
	gocaddy.RegisterModule(JWTBearerHandler{})
}

// JWTBearerHandlerConfig is the JSON-serialisable configuration.
type JWTBearerHandlerConfig struct {
	JWKSURL     string   `json:"jwks_url"`
	Issuer      string   `json:"issuer"`
	Audience    string   `json:"audience"`
	PublicPaths []string `json:"public_paths"`
}

// JWTBearerHandler is a Caddy HTTP handler module that validates JWT bearer
// tokens from the Authorization header against a JWKS endpoint.
type JWTBearerHandler struct {
	Config JWTBearerHandlerConfig `json:"config"`
	logger *slog.Logger
	jwks   *jose.JSONWebKeySet
}

// CaddyModule returns the Caddy module information.
func (JWTBearerHandler) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "http.handlers.jwt_bearer",
		New: func() gocaddy.Module { return new(JWTBearerHandler) },
	}
}

// Provision sets up the handler.
func (h *JWTBearerHandler) Provision(_ gocaddy.Context) error {
	h.logger = slog.Default()
	return nil
}

// UnmarshalJSON implements custom unmarshalling to extract config.
func (h *JWTBearerHandler) UnmarshalJSON(data []byte) error {
	// Try nested config first.
	var nested struct {
		Config JWTBearerHandlerConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &nested); err == nil && nested.Config.JWKSURL != "" {
		h.Config = nested.Config
		return nil
	}
	// Try flat structure.
	return json.Unmarshal(data, &h.Config)
}

// fetchJWKS fetches the JWKS from the configured URL.
func (h *JWTBearerHandler) fetchJWKS() (*jose.JSONWebKeySet, error) {
	if h.jwks != nil {
		return h.jwks, nil
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(h.Config.JWKSURL) //nolint:gosec // URL is from trusted config
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading JWKS: %w", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("parsing JWKS: %w", err)
	}

	h.jwks = &jwks
	return h.jwks, nil
}

// isPublicPath checks if the request path matches any public path pattern.
func (h *JWTBearerHandler) isPublicPath(reqPath string) bool {
	for _, p := range h.Config.PublicPaths {
		if strings.HasSuffix(p, "/*") {
			prefix := strings.TrimSuffix(p, "/*")
			if strings.HasPrefix(reqPath, prefix) {
				return true
			}
		}
		matched, _ := path.Match(p, reqPath)
		if matched {
			return true
		}
		if p == reqPath {
			return true
		}
	}
	return false
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *JWTBearerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Skip public paths.
	if h.isPublicPath(r.URL.Path) {
		return next.ServeHTTP(w, r)
	}

	// Extract Bearer token.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"missing or invalid Authorization header"}`)) //nolint:errcheck
		return nil
	}
	rawToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Fetch JWKS.
	jwks, err := h.fetchJWKS()
	if err != nil {
		h.logger.Error("jwt_bearer: JWKS fetch failed", slog.String("error", err.Error()))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"auth_unavailable","message":"identity provider unreachable"}`)) //nolint:errcheck
		return nil
	}

	// Parse and verify.
	tok, err := josejwt.ParseSigned(rawToken, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token","message":"failed to parse JWT"}`)) //nolint:errcheck
		return nil
	}

	if len(tok.Headers) == 0 || tok.Headers[0].KeyID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token","message":"JWT missing key ID"}`)) //nolint:errcheck
		return nil
	}

	keys := jwks.Key(tok.Headers[0].KeyID)
	if len(keys) == 0 {
		// Refresh JWKS once for key rotation.
		h.jwks = nil
		jwks, err = h.fetchJWKS()
		if err != nil || len(jwks.Key(tok.Headers[0].KeyID)) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_token","message":"signing key not found"}`)) //nolint:errcheck
			return nil
		}
		keys = jwks.Key(tok.Headers[0].KeyID)
	}

	var claims josejwt.Claims
	var custom map[string]any
	if err := tok.Claims(keys[0].Key, &claims, &custom); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_signature","message":"JWT signature verification failed"}`)) //nolint:errcheck
		return nil
	}

	// Validate standard claims.
	expected := josejwt.Expected{
		Issuer:      h.Config.Issuer,
		AnyAudience: josejwt.Audience{h.Config.Audience},
		Time:        time.Now(),
	}
	if err := claims.ValidateWithLeeway(expected, 30*time.Second); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		msg := fmt.Sprintf(`{"error":"token_invalid","message":"%s"}`, err.Error())
		_, _ = w.Write([]byte(msg)) //nolint:errcheck
		return nil
	}

	// Inject identity headers for the upstream app.
	if claims.Subject != "" {
		r.Header.Set("X-User-Id", claims.Subject)
	}
	if email, ok := custom["email"].(string); ok {
		r.Header.Set("X-User-Email", email)
	}
	if name, ok := custom["name"].(string); ok {
		r.Header.Set("X-User-Name", name)
	}
	if role, ok := custom["role"].(string); ok {
		r.Header.Set("X-User-Role", role)
	}

	return next.ServeHTTP(w, r)
}

// Interface guards.
var (
	_ caddyhttp.MiddlewareHandler = (*JWTBearerHandler)(nil)
	_ gocaddy.Module              = (*JWTBearerHandler)(nil)
	_ gocaddy.Provisioner         = (*JWTBearerHandler)(nil)
)
