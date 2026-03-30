package jwt

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-jose/go-jose/v4"
)

// DevJWKSPath is the URL path at which the local dev JWKS endpoint is served.
const DevJWKSPath = "/_vibewarden/jwks.json"

// DevKID is the key identifier used for the auto-generated dev signing key.
const DevKID = "vibewarden-dev-1"

// DevIssuer is the default JWT issuer value for local dev mode.
const DevIssuer = "vibewarden-dev"

// DevAudience is the default JWT audience value for local dev mode.
const DevAudience = "dev"

// JWKSHandler is an http.Handler that serves the JSON Web Key Set for the
// local dev RSA signing key in JWK format. It is mounted at DevJWKSPath by
// the Caddy route contributed by the auth plugin when jwks_url is empty.
type JWKSHandler struct {
	body []byte
}

// NewJWKSHandler creates a new JWKSHandler that serves the public key of key
// as a single-entry JWKS JSON document.
//
// The key is serialised once at construction time; the handler is immutable
// and safe for concurrent use by multiple goroutines.
func NewJWKSHandler(key *rsa.PublicKey) (*JWKSHandler, error) {
	jwk := jose.JSONWebKey{
		Key:       key,
		KeyID:     DevKID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	body, err := json.Marshal(jwks)
	if err != nil {
		return nil, fmt.Errorf("jwks handler: serialising key set: %w", err)
	}

	return &JWKSHandler{body: body}, nil
}

// ServeHTTP implements http.Handler.
// It responds with the pre-serialised JWKS JSON and the appropriate
// Content-Type header. The response is always HTTP 200.
func (h *JWKSHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.body)
}
