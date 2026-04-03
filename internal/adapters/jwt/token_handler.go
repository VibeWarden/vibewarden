package jwt

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// DevTokenPath is the URL path at which the dev token endpoint is served.
// It is only active when auth.mode is "jwt" and no external jwks_url is configured.
const DevTokenPath = "/_vibewarden/token"

// tokenResponse is the JSON body returned by the dev token endpoint.
type tokenResponse struct {
	Token string `json:"token"`
}

// TokenHandler is an http.Handler that issues signed dev JWTs on demand.
// It signs tokens using the dev RSA private key and accepts claims via query
// parameters. It is mounted at DevTokenPath by the Caddy route contributed by
// the auth plugin when jwks_url is empty.
//
// TokenHandler is immutable and safe for concurrent use by multiple goroutines.
type TokenHandler struct {
	keyPair *DevKeyPair
}

// NewTokenHandler creates a new TokenHandler that signs tokens with the dev
// RSA private key in keyPair.
func NewTokenHandler(keyPair *DevKeyPair) *TokenHandler {
	return &TokenHandler{keyPair: keyPair}
}

// ServeHTTP implements http.Handler.
//
// Accepted query parameters:
//   - sub    — JWT subject claim (default: "dev-user")
//   - email  — email claim (default: "dev@localhost")
//   - name   — name claim (default: "Dev User")
//   - role   — role claim (default: "user")
//   - expires — token lifetime as a Go duration string (default: "1h")
//
// On success it responds HTTP 200 with {"token": "<compact-jwt>"}.
// On invalid input it responds HTTP 400 with a plain-text error message.
func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	sub := q.Get("sub")
	if sub == "" {
		sub = "dev-user"
	}
	email := q.Get("email")
	if email == "" {
		email = "dev@localhost"
	}
	name := q.Get("name")
	if name == "" {
		name = "Dev User"
	}
	role := q.Get("role")
	if role == "" {
		role = "user"
	}

	expiresStr := q.Get("expires")
	if expiresStr == "" {
		expiresStr = "1h"
	}
	ttl, err := time.ParseDuration(expiresStr)
	if err != nil {
		http.Error(w, "invalid expires parameter: "+err.Error(), http.StatusBadRequest)
		return
	}
	if ttl <= 0 {
		http.Error(w, "expires must be a positive duration", http.StatusBadRequest)
		return
	}

	// signDevTokenInternal reuses the same signing logic as the vibew token CLI
	// command, keeping a single code path for token generation.
	token, err := signDevToken(context.Background(), h.keyPair, sub, email, name, role, ttl)
	if err != nil {
		http.Error(w, "signing token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokenResponse{Token: token})
}
