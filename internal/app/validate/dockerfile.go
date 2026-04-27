package validate

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// CheckDockerfile parses the EXPOSE directives in <projectRoot>/Dockerfile and
// compares the last valid port against cfg.Upstream.Port.
//
// Skip conditions (no row emitted):
//   - Dockerfile is absent.
//   - No valid EXPOSE directive is found.
//   - The EXPOSE token is not a base-10 integer in [1, 65535].
//   - The ports match.
//
// Multi-line continuation (\) is not supported; such lines are treated as
// malformed and skipped, matching the architect's directive.
func CheckDockerfile(_ context.Context, projectRoot string, cfg *config.Config, _ bool) Result {
	dockerfilePath := filepath.Join(projectRoot, "Dockerfile")
	f, err := os.Open(dockerfilePath) //nolint:gosec // projectRoot is the project root provided by the caller
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Skip: true}
		}
		// Unreadable Dockerfile: skip silently — validate is a best-effort check.
		return Result{Skip: true}
	}
	defer func() { _ = f.Close() }()

	lastPort := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.ToLower(fields[0]) != "expose" {
			continue
		}
		// Strip protocol suffix (e.g. "3000/tcp" → "3000").
		token := fields[1]
		if idx := strings.Index(token, "/"); idx >= 0 {
			token = token[:idx]
		}
		port, err := strconv.Atoi(token)
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		lastPort = port
	}

	if lastPort == 0 {
		// No valid EXPOSE found — skip silently.
		return Result{Skip: true}
	}

	if lastPort == cfg.Upstream.Port {
		// Ports match — no row needed.
		return Result{Skip: true}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: fmt.Sprintf(
			"Dockerfile EXPOSE %d does not match upstream.port %d — update one to match",
			lastPort,
			cfg.Upstream.Port,
		),
	}
}
