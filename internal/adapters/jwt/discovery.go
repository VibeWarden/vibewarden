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

// maxDiscoveryBodySize is the maximum number of bytes read from an OIDC
// discovery response body. Protects against excessively large responses.
const maxDiscoveryBodySize = 1 << 20 // 1 MiB

// OIDCConfiguration is the subset of the OpenID Connect Discovery 1.0 response
// fields that VibeWarden requires. The full specification is at
// https://openid.net/specs/openid-connect-discovery-1_0.html.
type OIDCConfiguration struct {
	// JwksURI is the URL of the JSON Web Key Set document.
	JwksURI string `json:"jwks_uri"`

	// Issuer is the issuer identifier for the OpenID Provider.
	Issuer string `json:"issuer"`
}

// DiscoverJWKSURL performs OIDC Discovery against the given issuer URL and
// returns the JWKS URI found in the provider's discovery document.
//
// It appends /.well-known/openid-configuration to the issuer URL and fetches
// the JSON document. A trailing slash on issuerURL is normalised away.
//
// If timeout is zero it defaults to 10 seconds.
func DiscoverJWKSURL(ctx context.Context, issuerURL string, timeout time.Duration) (string, error) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	issuerURL = strings.TrimSuffix(issuerURL, "/")
	discoveryURL := issuerURL + "/.well-known/openid-configuration"

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("oidc discovery: creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc discovery: fetching %s: %w", discoveryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oidc discovery: endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryBodySize))
	if err != nil {
		return "", fmt.Errorf("oidc discovery: reading response: %w", err)
	}

	var cfg OIDCConfiguration
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", fmt.Errorf("oidc discovery: parsing configuration: %w", err)
	}

	if cfg.JwksURI == "" {
		return "", fmt.Errorf("oidc discovery: configuration at %s missing jwks_uri", discoveryURL)
	}

	return cfg.JwksURI, nil
}
