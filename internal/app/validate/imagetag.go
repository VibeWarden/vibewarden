package validate

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// CheckImageTag reads <projectRoot>/.env and checks whether the
// VIBEWARDEN_APP_IMAGE value matches the tag that vibew bundle would produce
// for the current config.
//
// Skip conditions (no row emitted):
//   - .env is absent.
//   - .env has no VIBEWARDEN_APP_IMAGE line (or the line is commented out).
//   - The value matches the expected tag.
//
// A mismatch means the deployed image tag is stale and the user should run
// vibew bundle --overwrite.
func CheckImageTag(_ context.Context, projectRoot string, cfg *config.Config, _ bool) Result {
	envPath := filepath.Join(projectRoot, ".env")
	f, err := os.Open(envPath) //nolint:gosec // projectRoot is the project root provided by the caller
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Skip: true}
		}
		return Result{Skip: true}
	}
	defer func() { _ = f.Close() }()

	imageValue := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if key == "VIBEWARDEN_APP_IMAGE" {
			imageValue = val
			// Keep scanning — use the last occurrence.
		}
	}

	if imageValue == "" {
		return Result{Skip: true}
	}

	// Derive the expected tag using the same logic as vibew bundle.
	// absConfigPath is synthesised from projectRoot; the basename is what matters.
	absConfigPath := filepath.Join(projectRoot, "vibewarden.yaml")
	projectName := bundle.DeriveProjectName(cfg, absConfigPath)
	expectedTag := projectName + "-app:latest"

	if imageValue == expectedTag {
		return Result{Skip: true}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: fmt.Sprintf(
			".env VIBEWARDEN_APP_IMAGE=%s does not match expected tag %s — run: vibew bundle --overwrite",
			imageValue,
			expectedTag,
		),
	}
}
