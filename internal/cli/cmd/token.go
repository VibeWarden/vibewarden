package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/spf13/cobra"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
)

// devTokenClaims holds the custom claims written into the dev JWT.
type devTokenClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// NewTokenCmd creates the "vibew token" subcommand.
//
// The command loads the dev RSA private key from .vibewarden/dev-keys/private.pem,
// signs a JWT with the supplied claims, and writes the token to stdout. When
// --json is given only the raw token string is printed, which makes the output
// suitable for shell interpolation:
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
func runToken(cmd *cobra.Command, keyDir, sub, email, name, role, expires string, jsonOut bool) error {
	dir, err := resolveKeyDir(keyDir)
	if err != nil {
		return err
	}

	privPath := filepath.Join(dir, jwtadapter.DevPrivateKeyFile)
	if _, err := os.Stat(privPath); err != nil {
		return fmt.Errorf(
			"dev keys not found at %s: run \"vibew dev\" or \"vibew generate\" first",
			privPath,
		)
	}

	kp, err := jwtadapter.LoadOrGenerateDevKeys(dir)
	if err != nil {
		return fmt.Errorf("loading dev key pair: %w", err)
	}

	ttl, err := time.ParseDuration(expires)
	if err != nil {
		return fmt.Errorf("invalid --expires value %q: %w", expires, err)
	}
	if ttl <= 0 {
		return fmt.Errorf("--expires must be a positive duration, got %q", expires)
	}

	token, err := SignDevToken(context.Background(), kp, sub, email, name, role, ttl)
	if err != nil {
		return fmt.Errorf("signing token: %w", err)
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

// SignDevToken signs a JWT with the dev RSA private key and returns the compact
// serialisation. It is a pure function (aside from time.Now()) and is exposed
// so package-level tests can call it directly with a pre-built key pair.
func SignDevToken(_ context.Context, kp *jwtadapter.DevKeyPair, sub, email, name, role string, ttl time.Duration) (string, error) {
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: kp.PrivateKey},
		(&jose.SignerOptions{}).
			WithType("JWT").
			WithHeader("kid", jwtadapter.DevKID),
	)
	if err != nil {
		return "", fmt.Errorf("creating signer: %w", err)
	}

	now := time.Now()
	std := josejwt.Claims{
		Issuer:   jwtadapter.DevIssuer,
		Audience: josejwt.Audience{jwtadapter.DevAudience},
		Subject:  sub,
		IssuedAt: josejwt.NewNumericDate(now),
		Expiry:   josejwt.NewNumericDate(now.Add(ttl)),
	}

	custom := devTokenClaims{
		Email: email,
		Name:  name,
		Role:  role,
	}

	raw, err := josejwt.Signed(sig).Claims(std).Claims(custom).Serialize()
	if err != nil {
		return "", fmt.Errorf("serialising token: %w", err)
	}

	return raw, nil
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
