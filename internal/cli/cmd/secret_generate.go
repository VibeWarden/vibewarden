package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

const (
	// defaultSecretLength is the default number of random bytes to generate.
	// 32 bytes = 256 bits of entropy, encoded as 64 hex characters.
	defaultSecretLength = 32

	// minSecretLength is the minimum allowed --length value.
	minSecretLength = 16

	// maxSecretLength is the maximum allowed --length value.
	maxSecretLength = 256
)

// newSecretGenerateCmd creates the `vibew secret generate` subcommand.
//
// It generates a cryptographically secure random token using crypto/rand and
// prints it as a lowercase hex string to stdout. The output is intentionally
// minimal (no trailing newline decoration) so it can be piped to other tools.
func newSecretGenerateCmd() *cobra.Command {
	var (
		length     int
		adminToken bool
		fleetKey   bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a cryptographically secure random secret",
		Long: `Generate a cryptographically secure random secret using crypto/rand.

The secret is printed as a lowercase hex string to stdout.

Flags --admin-token and --fleet-key are convenience aliases that generate
a 32-byte (64 hex character) token with the matching environment variable
name prefixed in the output, making it easy to pipe into a .env file.

Examples:
  # Generic 32-byte secret
  vibew secret generate

  # 64-byte secret
  vibew secret generate --length 64

  # Admin API token (ready to paste into .env)
  vibew secret generate --admin-token

  # Fleet dashboard key
  vibew secret generate --fleet-key`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --admin-token and --fleet-key are mutually exclusive.
			if adminToken && fleetKey {
				return fmt.Errorf("--admin-token and --fleet-key are mutually exclusive")
			}

			n := length
			var prefix string

			switch {
			case adminToken:
				n = defaultSecretLength
				prefix = "VIBEWARDEN_ADMIN_TOKEN="
			case fleetKey:
				n = defaultSecretLength
				prefix = "VIBEWARDEN_FLEET_KEY="
			}

			if n < minSecretLength || n > maxSecretLength {
				return fmt.Errorf("--length must be between %d and %d, got %d", minSecretLength, maxSecretLength, n)
			}

			token, err := generateHexSecret(n)
			if err != nil {
				return fmt.Errorf("generating secret: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s%s\n", prefix, token)
			return nil
		},
	}

	cmd.Flags().IntVar(&length, "length", defaultSecretLength, "number of random bytes to generate (result is 2x hex chars)")
	cmd.Flags().BoolVar(&adminToken, "admin-token", false, "generate a 32-byte admin API token (outputs VIBEWARDEN_ADMIN_TOKEN=...)")
	cmd.Flags().BoolVar(&fleetKey, "fleet-key", false, "generate a 32-byte fleet dashboard key (outputs VIBEWARDEN_FLEET_KEY=...)")

	return cmd
}

// generateHexSecret generates n cryptographically random bytes and returns
// them encoded as a lowercase hex string of length 2*n.
//
// It uses crypto/rand which reads from the OS CSPRNG (/dev/urandom on Linux,
// CryptGenRandom on Windows). An error is returned only if the OS entropy
// source is unavailable — practically never on a healthy system.
func generateHexSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
