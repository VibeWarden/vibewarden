// Package jwt implements the IdentityProvider port for JWT/OIDC token validation.
// It uses go-jose/v4 for JWT parsing and signature verification, with a pluggable
// JWKSFetcher for key retrieval and caching.
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

// reservedClaims is the set of standard JWT claim names that are not forwarded
// into Identity.Claims(). These are handled separately via standard fields.
var reservedClaims = map[string]bool{
	"sub": true, "iss": true, "aud": true, "exp": true,
	"iat": true, "nbf": true, "jti": true, "typ": true,
}

// Config holds configuration for the JWT identity adapter.
type Config struct {
	// JWKSURL is the URL to fetch the JSON Web Key Set.
	// Either JWKSURL or IssuerURL must be set. When both are set, JWKSURL takes
	// precedence and OIDC Discovery is skipped.
	JWKSURL string

	// IssuerURL is the OIDC issuer URL for auto-discovery.
	// When set (and JWKSURL is empty), the JWKS URL is discovered from
	// /.well-known/openid-configuration.
	IssuerURL string

	// Issuer is the expected "iss" claim value. Required.
	Issuer string

	// Audience is the expected "aud" claim value. Required.
	Audience string

	// ClaimsToHeaders maps JWT claim names to HTTP header names.
	// These headers are injected into requests forwarded to the upstream application.
	// Example: {"roles": "X-User-Roles", "name": "X-User-Name"}
	// The standard sub, email, and email_verified claims are always mapped to
	// X-User-Id, X-User-Email, and X-User-Verified by IdentityHeadersMiddleware.
	ClaimsToHeaders map[string]string

	// AllowedAlgorithms restricts which signing algorithms are accepted.
	// Defaults to ["RS256", "ES256"] when empty.
	// Never include "none" or symmetric algorithms (HS256, etc.) in production.
	AllowedAlgorithms []string

	// CacheTTL is how long to cache the JWKS before refreshing.
	// Defaults to 1 hour when zero.
	CacheTTL time.Duration
}

// Adapter implements ports.IdentityProvider for JWT/OIDC Bearer token authentication.
//
// It extracts the Bearer token from the Authorization header, validates the
// JWT signature against keys fetched from the JWKS endpoint, and validates
// standard claims (iss, aud, exp, iat, nbf). On success it returns an
// identity.Identity whose claims map contains all non-reserved JWT claims.
type Adapter struct {
	config  Config
	fetcher ports.JWKSFetcher
	logger  *slog.Logger
}

// NewAdapter creates a new JWT identity adapter.
//
// Returns an error if the configuration is invalid (missing required fields).
// The fetcher must not be nil; use NewHTTPJWKSFetcher to create one.
func NewAdapter(cfg Config, fetcher ports.JWKSFetcher, logger *slog.Logger) (*Adapter, error) {
	if fetcher == nil {
		return nil, errors.New("jwt adapter: fetcher must not be nil")
	}
	if cfg.JWKSURL == "" && cfg.IssuerURL == "" {
		return nil, errors.New("jwt adapter: either jwks_url or issuer_url is required")
	}
	if cfg.Issuer == "" {
		return nil, errors.New("jwt adapter: issuer is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("jwt adapter: audience is required")
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
// Returns "jwt" as the provider identifier.
func (a *Adapter) Name() string { return "jwt" }

// Authenticate implements ports.IdentityProvider.
//
// It extracts the Bearer token from the Authorization header, validates the JWT
// signature and standard claims, and returns an AuthResult with the user's identity.
//
// Failure reasons:
//   - "no_credentials": no Authorization header, or header is not Bearer, or token is empty
//   - "invalid_token": JWT could not be parsed
//   - "invalid_signature": signature verification failed, or key ID not found
//   - "token_expired": token exp claim is in the past
//   - "token_not_yet_valid": token nbf claim is in the future
//   - "invalid_issuer": iss claim does not match configured issuer
//   - "invalid_audience": aud claim does not contain configured audience
//   - "provider_unavailable": JWKS endpoint could not be reached
func (a *Adapter) Authenticate(ctx context.Context, r *http.Request) identity.AuthResult {
	// Extract Bearer token from Authorization header.
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

	// Parse the JWT to extract the key ID (kid) without verifying the signature.
	// The jose library's ParseSigned validates the algorithm against AllowedAlgorithms.
	tok, err := josejwt.ParseSigned(rawToken, algorithmsToJOSE(a.config.AllowedAlgorithms))
	if err != nil {
		a.logger.WarnContext(ctx, "jwt: failed to parse token",
			slog.String("error", err.Error()),
		)
		return identity.Failure("invalid_token", "failed to parse JWT: "+err.Error())
	}

	if len(tok.Headers) == 0 {
		return identity.Failure("invalid_token", "JWT has no headers")
	}
	kid := tok.Headers[0].KeyID

	// Retrieve the signing key from the JWKS fetcher.
	key, err := a.fetcher.GetKey(ctx, kid)
	if err != nil {
		// Distinguish between a JWKS transport/parse failure (provider unavailable)
		// and a key-not-found error (invalid signature). A key-not-found error from
		// HTTPJWKSFetcher wraps the message "key not found: <kid>", while transport
		// failures have other messages. We check for context errors first, then use
		// the error message to distinguish the two cases.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			a.logger.ErrorContext(ctx, "jwt: JWKS fetch timed out or cancelled",
				slog.String("kid", kid),
				slog.String("error", err.Error()),
			)
			return identity.Failure("provider_unavailable", "JWKS fetch timed out")
		}
		// If the error message contains "key not found", it means the JWKS was
		// fetched successfully but the specific key ID was not present.
		// Any other error means the JWKS endpoint could not be reached.
		if isKeyNotFoundError(err) {
			a.logger.WarnContext(ctx, "jwt: key not found",
				slog.String("kid", kid),
				slog.String("error", err.Error()),
			)
			return identity.Failure("invalid_signature", "signing key not found: "+kid)
		}
		a.logger.ErrorContext(ctx, "jwt: JWKS provider unavailable",
			slog.String("kid", kid),
			slog.String("error", err.Error()),
		)
		return identity.Failure("provider_unavailable", "JWKS provider unavailable: "+err.Error())
	}

	// Verify the JWT signature and extract claims.
	var stdClaims josejwt.Claims
	var customClaims map[string]any
	if err := tok.Claims(key.Key, &stdClaims, &customClaims); err != nil {
		a.logger.WarnContext(ctx, "jwt: signature verification failed",
			slog.String("error", err.Error()),
		)
		return identity.Failure("invalid_signature", "signature verification failed: "+err.Error())
	}

	// Validate standard claims: iss, aud, exp, nbf.
	expected := josejwt.Expected{
		Issuer:      a.config.Issuer,
		AnyAudience: josejwt.Audience{a.config.Audience},
		Time:        time.Now(),
	}
	if err := stdClaims.ValidateWithLeeway(expected, 0); err != nil {
		reason := classifyClaimsError(err)
		a.logger.WarnContext(ctx, "jwt: claims validation failed",
			slog.String("reason", reason),
			slog.String("error", err.Error()),
		)
		return identity.Failure(reason, err.Error())
	}

	// Extract well-known claims for Identity fields.
	email, _ := customClaims["email"].(string)
	emailVerified, _ := customClaims["email_verified"].(bool)

	// Build the non-reserved claims map for Identity.Claims().
	filteredClaims := make(map[string]any, len(customClaims))
	for k, v := range customClaims {
		if !reservedClaims[k] {
			filteredClaims[k] = v
		}
	}

	ident, err := identity.NewIdentity(
		stdClaims.Subject,
		email,
		"jwt",
		emailVerified,
		filteredClaims,
	)
	if err != nil {
		return identity.Failure("invalid_identity", err.Error())
	}

	a.logger.DebugContext(ctx, "jwt: token validated",
		slog.String("subject", stdClaims.Subject),
		slog.String("issuer", string(stdClaims.Issuer)),
	)

	return identity.Success(ident)
}

// ClaimsToHeaders returns the configured claims-to-headers mapping.
// The middleware uses this to inject additional JWT claims as request headers.
func (a *Adapter) ClaimsToHeaders() map[string]string {
	return a.config.ClaimsToHeaders
}

// classifyClaimsError maps a go-jose claims validation error to a machine-readable
// reason code used in AuthResult.Reason.
func classifyClaimsError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "token is expired"):
		return "token_expired"
	case strings.Contains(msg, "token not valid yet"):
		return "token_not_yet_valid"
	case strings.Contains(msg, "issuer"), strings.Contains(msg, "iss"):
		return "invalid_issuer"
	case strings.Contains(msg, "audience"), strings.Contains(msg, "aud"):
		return "invalid_audience"
	default:
		return "token_invalid"
	}
}

// algorithmsToJOSE converts a slice of algorithm name strings to the strongly
// typed jose.SignatureAlgorithm slice required by go-jose's ParseSigned function.
func algorithmsToJOSE(algs []string) []jose.SignatureAlgorithm {
	out := make([]jose.SignatureAlgorithm, len(algs))
	for i, a := range algs {
		out[i] = jose.SignatureAlgorithm(a)
	}
	return out
}

// isKeyNotFoundError reports whether the error came from a successful JWKS fetch
// where the requested key ID was simply not present. This is distinguished from
// a transport or parse error by the error message prefix set by HTTPJWKSFetcher.
func isKeyNotFoundError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "jwks: key not found:")
}
