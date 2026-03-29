package events

import (
	"fmt"
	"time"
)

// JWT-specific event type constants.
// These values are stable and form part of the public schema contract.
const (
	// EventTypeJWTValid is emitted when a JWT token passes all validation checks.
	EventTypeJWTValid = "auth.jwt_valid"

	// EventTypeJWTInvalid is emitted when a JWT token fails validation (bad
	// signature, wrong issuer, wrong audience, parse error, etc.).
	EventTypeJWTInvalid = "auth.jwt_invalid"

	// EventTypeJWTExpired is emitted when a JWT token is structurally valid but
	// has passed its expiry time.
	EventTypeJWTExpired = "auth.jwt_expired"

	// EventTypeJWKSRefresh is emitted each time the JWKS cache is successfully
	// refreshed from the remote endpoint.
	EventTypeJWKSRefresh = "auth.jwks_refresh"

	// EventTypeJWKSError is emitted when fetching or parsing the JWKS fails,
	// making JWT validation impossible until the next successful refresh.
	EventTypeJWKSError = "auth.jwks_error"
)

// JWTValidParams contains the parameters needed to construct an auth.jwt_valid event.
type JWTValidParams struct {
	// Method is the HTTP method of the authenticated request (e.g. "GET").
	Method string

	// Path is the URL path of the authenticated request.
	Path string

	// Subject is the "sub" claim value from the validated token.
	Subject string

	// Issuer is the "iss" claim value from the validated token.
	Issuer string

	// Audience is the "aud" claim value that was validated against.
	Audience string
}

// NewJWTValid creates an auth.jwt_valid event indicating that a request
// carried a structurally valid, correctly signed, and unexpired JWT token.
func NewJWTValid(params JWTValidParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeJWTValid,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"JWT validated: %s %s (sub=%s)",
			params.Method, params.Path, params.Subject,
		),
		Payload: map[string]any{
			"method":   params.Method,
			"path":     params.Path,
			"subject":  params.Subject,
			"issuer":   params.Issuer,
			"audience": params.Audience,
		},
	}
}

// JWTInvalidParams contains the parameters needed to construct an auth.jwt_invalid event.
type JWTInvalidParams struct {
	// Method is the HTTP method of the rejected request (e.g. "GET").
	Method string

	// Path is the URL path of the rejected request.
	Path string

	// Reason is a machine-readable failure code (e.g. "invalid_signature",
	// "invalid_issuer", "invalid_audience", "invalid_token").
	Reason string

	// Detail is an optional additional detail string (e.g. an error message).
	// May be empty.
	Detail string
}

// NewJWTInvalid creates an auth.jwt_invalid event indicating that a JWT token
// failed validation for any reason other than expiry.
func NewJWTInvalid(params JWTInvalidParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeJWTInvalid,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"JWT validation failed: %s",
			params.Reason,
		),
		Payload: map[string]any{
			"method": params.Method,
			"path":   params.Path,
			"reason": params.Reason,
			"detail": params.Detail,
		},
	}
}

// JWTExpiredParams contains the parameters needed to construct an auth.jwt_expired event.
type JWTExpiredParams struct {
	// Method is the HTTP method of the rejected request (e.g. "GET").
	Method string

	// Path is the URL path of the rejected request.
	Path string

	// Subject is the "sub" claim value from the expired token.
	Subject string

	// ExpiredAt is the time at which the token expired (value of the "exp" claim).
	ExpiredAt time.Time
}

// NewJWTExpired creates an auth.jwt_expired event indicating that a JWT token
// was structurally valid but has passed its expiry time.
func NewJWTExpired(params JWTExpiredParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeJWTExpired,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"JWT expired at %s for subject %s",
			params.ExpiredAt.UTC().Format(time.RFC3339),
			params.Subject,
		),
		Payload: map[string]any{
			"method":     params.Method,
			"path":       params.Path,
			"subject":    params.Subject,
			"expired_at": params.ExpiredAt.UTC().Format(time.RFC3339),
		},
	}
}

// JWKSRefreshParams contains the parameters needed to construct an auth.jwks_refresh event.
type JWKSRefreshParams struct {
	// JWKSURL is the URL from which the key set was fetched.
	JWKSURL string

	// KeyCount is the number of keys in the refreshed key set.
	KeyCount int
}

// NewJWKSRefresh creates an auth.jwks_refresh event indicating that the
// JWKS cache was successfully refreshed.
func NewJWKSRefresh(params JWKSRefreshParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeJWKSRefresh,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"JWKS refreshed from %s (%d keys)",
			params.JWKSURL, params.KeyCount,
		),
		Payload: map[string]any{
			"jwks_url":  params.JWKSURL,
			"key_count": params.KeyCount,
		},
	}
}

// JWKSErrorParams contains the parameters needed to construct an auth.jwks_error event.
type JWKSErrorParams struct {
	// JWKSURL is the URL that could not be reached.
	JWKSURL string

	// Detail is the error message describing what went wrong.
	Detail string
}

// NewJWKSError creates an auth.jwks_error event indicating that fetching
// or parsing the JWKS failed.
func NewJWKSError(params JWKSErrorParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeJWKSError,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"JWKS fetch failed from %s: %s",
			params.JWKSURL, params.Detail,
		),
		Payload: map[string]any{
			"jwks_url": params.JWKSURL,
			"detail":   params.Detail,
		},
	}
}
