package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
)

// devSidecarTokenURL is the URL of the dev token endpoint on the sidecar.
// The sidecar self-signs TLS, so we skip certificate verification for localhost.
const devSidecarTokenURL = "https://localhost:8443" + jwtadapter.DevTokenPath

// NewTokenCmd creates the "vibew token" subcommand.
//
// The command first tries the sidecar at https://localhost:8443/_vibewarden/token.
// When the sidecar is not reachable (e.g. before "vibew dev" has been run) it
// falls back to loading the dev RSA private key from .vibewarden/dev-keys/ and
// signing locally. The --json flag prints only the raw token for shell
// interpolation:
//
//	curl -H "Authorization: Bearer $(vibew token)" https://localhost:8443/api/me
func NewTokenCmd() *cobra.Command {
	var (
		sub     string
		email   string
		name    string
		role    string
		expires string
		jsonOut bool
		keyDir  string
	)

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Generate a signed dev JWT for local testing",
		Long: `Generate a signed JWT using the local dev RSA private key.

The key must exist at .vibewarden/dev-keys/private.pem. Run "vibew dev"
or "vibew generate" first if the key is missing — VibeWarden creates the
key pair automatically on first run.

The generated token is signed with RS256, uses kid=` + jwtadapter.DevKID + `,
iss=` + jwtadapter.DevIssuer + `, and aud=` + jwtadapter.DevAudience + `.

Examples:
  vibew token
  vibew token --sub user-123 --email alice@test.com --role admin
  vibew token --expires 24h
  vibew token --json
  curl https://localhost:8443/api/me \
    -H "Authorization: Bearer $(vibew token --json)"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToken(cmd, keyDir, sub, email, name, role, expires, jsonOut)
		},
	}

	cmd.Flags().StringVar(&sub, "sub", "dev-user", "Subject (user ID) claim")
	cmd.Flags().StringVar(&email, "email", "dev@localhost", "Email claim")
	cmd.Flags().StringVar(&name, "name", "Dev User", "Name claim")
	cmd.Flags().StringVar(&role, "role", "user", "Role claim")
	cmd.Flags().StringVar(&expires, "expires", "1h", "Token lifetime (Go duration, e.g. 1h, 30m, 24h)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print raw token only (for shell interpolation)")
	cmd.Flags().StringVar(&keyDir, "key-dir", "", "Directory containing the dev key pair (default: .vibewarden/dev-keys)")

	return cmd
}

// runToken is the testable core of the token command. It is separated from the
// cobra RunE closure so it can be exercised by unit tests without constructing
// a cobra.Command.
//
// It validates the --expires flag first (to surface bad input before any I/O),
// then attempts to get a token from the running sidecar. If the sidecar is
// unreachable it falls back to local key signing.
func runToken(cmd *cobra.Command, keyDir, sub, email, name, role, expires string, jsonOut bool) error {
	ttl, err := time.ParseDuration(expires)
	if err != nil {
		return fmt.Errorf("invalid --expires value %q: %w", expires, err)
	}
	if ttl <= 0 {
		return fmt.Errorf("--expires must be a positive duration, got %q", expires)
	}

	// Attempt to obtain a token from the running sidecar. This is the preferred
	// path when "vibew dev" is running because the sidecar holds the private key.
	token, err := fetchTokenFromSidecar(context.Background(), sub, email, name, role, ttl)
	if err != nil {
		// Sidecar is not reachable — fall back to local key signing.
		token, err = signTokenLocally(keyDir, sub, email, name, role, ttl)
		if err != nil {
			return err
		}
	}

	if jsonOut {
		fmt.Fprintln(cmd.OutOrStdout(), token)
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), token)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "Hint: use this token with curl:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  curl https://localhost:8443/api/me \\\n")
	fmt.Fprintf(cmd.OutOrStdout(), "    -H \"Authorization: Bearer %s\"\n", token)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "Or inline via shell substitution:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  curl https://localhost:8443/api/me \\\n")
	fmt.Fprintf(cmd.OutOrStdout(), "    -H \"Authorization: Bearer $(vibew token --json)\"\n")

	return nil
}

// fetchTokenFromSidecar calls the running sidecar's dev token endpoint.
// It uses a short timeout and skips TLS verification because the sidecar uses a
// self-signed certificate. Returns an error when the sidecar is unreachable or
// returns a non-200 response.
func fetchTokenFromSidecar(ctx context.Context, sub, email, name, role string, ttl time.Duration) (string, error) {
	params := url.Values{}
	params.Set("sub", sub)
	params.Set("email", email)
	params.Set("name", name)
	params.Set("role", role)
	params.Set("expires", ttl.String())

	reqURL := devSidecarTokenURL + "?" + params.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("building sidecar token request: %w", err)
	}

	// Accept self-signed certificates — the sidecar always runs on localhost.
	//nolint:gosec // localhost self-signed TLS is intentional for dev mode
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sidecar unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sidecar returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding sidecar response: %w", err)
	}
	if body.Token == "" {
		return "", fmt.Errorf("sidecar returned empty token")
	}

	return body.Token, nil
}

// signTokenLocally loads the dev private key from keyDir and signs a JWT.
// This is the fallback path when the sidecar is not running.
func signTokenLocally(keyDir, sub, email, name, role string, ttl time.Duration) (string, error) {
	dir, err := resolveKeyDir(keyDir)
	if err != nil {
		return "", err
	}

	privPath := filepath.Join(dir, jwtadapter.DevPrivateKeyFile)
	if _, err := os.Stat(privPath); err != nil {
		return "", fmt.Errorf(
			"dev keys not found at %s: run \"vibew dev\" first (sidecar was also unreachable)",
			privPath,
		)
	}

	kp, err := jwtadapter.LoadOrGenerateDevKeys(dir)
	if err != nil {
		return "", fmt.Errorf("loading dev key pair: %w", err)
	}

	token, err := SignDevToken(context.Background(), kp, sub, email, name, role, ttl)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}

	return token, nil
}

// SignDevToken signs a JWT with the dev RSA private key and returns the compact
// serialisation. It is a pure function (aside from time.Now()) and is exposed
// so package-level tests can call it directly with a pre-built key pair.
//
// It delegates to the jwt adapter package's internal signing function so that
// the HTTP token handler and the CLI command share a single implementation.
func SignDevToken(ctx context.Context, kp *jwtadapter.DevKeyPair, sub, email, name, role string, ttl time.Duration) (string, error) {
	return jwtadapter.SignToken(ctx, kp, sub, email, name, role, ttl)
}

// resolveKeyDir returns the absolute path to the dev key directory.
// When override is non-empty it is used as-is. Otherwise the default
// relative path ".vibewarden/dev-keys" is resolved against the working directory.
func resolveKeyDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	return filepath.Join(wd, jwtadapter.DevKeyDir), nil
}
