package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

// diagnoseHealthFailure runs SSH commands to determine why the health check
// failed. It checks container status, looks for TLS errors in logs, probes
// the upstream directly, and collects recent Caddy logs. The result is a
// classified HealthDiagnostic value object.
//
// This method reuses the existing ports.RemoteExecutor — no new ports are
// introduced.
func (s *Service) diagnoseHealthFailure(ctx context.Context, remoteDir string, port int, tlsEnabled bool) health.Diagnostic {
	// Collect container status.
	containerStatus := s.getContainerStatus(ctx, remoteDir)

	// Collect recent sidecar logs (last 30 lines).
	caddyLogs := s.getSidecarLogs(ctx, remoteDir)

	// Classify: check container health first (most severe).
	if category, detail := classifyContainerStatus(containerStatus); category != "" {
		return health.NewDiagnostic(category, containerStatus, caddyLogs, detail)
	}

	// Classify: check for TLS errors in logs.
	if detail := detectTLSError(caddyLogs); detail != "" {
		return health.NewDiagnostic(health.CategoryTLSError, containerStatus, caddyLogs, detail)
	}

	// Classify: check if upstream is unreachable.
	if detail := s.detectUpstreamUnreachable(ctx, remoteDir, port, tlsEnabled); detail != "" {
		return health.NewDiagnostic(health.CategoryUpstreamUnreachable, containerStatus, caddyLogs, detail)
	}

	// Fallback: timeout with no specific signal.
	return health.NewDiagnostic(
		health.CategoryTimeout,
		containerStatus,
		caddyLogs,
		"Health check timed out. The sidecar may still be starting. Run: vibew deploy status",
	)
}

// getContainerStatus fetches docker compose ps output for the project.
func (s *Service) getContainerStatus(ctx context.Context, remoteDir string) string {
	cmd := fmt.Sprintf("cd %s && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null", remoteDir)
	output, err := s.executor.Run(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("(failed to fetch status: %v)", err)
	}
	return strings.TrimSpace(output)
}

// getSidecarLogs fetches the last 30 lines of the vibewarden service logs.
func (s *Service) getSidecarLogs(ctx context.Context, remoteDir string) string {
	cmd := fmt.Sprintf("cd %s && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null", remoteDir)
	output, err := s.executor.Run(ctx, cmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

// classifyContainerStatus determines if any container is not running or
// unhealthy based on the docker compose ps output. Returns the category and
// a detail string, or empty strings if all containers look healthy.
func classifyContainerStatus(status string) (health.FailureCategory, string) {
	if status == "" {
		return health.CategoryContainerUnhealthy, "No containers found. Docker Compose may have failed to start."
	}

	lower := strings.ToLower(status)

	// Check for exited/dead containers.
	if strings.Contains(lower, "exited") || strings.Contains(lower, "dead") {
		return health.CategoryContainerUnhealthy,
			"One or more containers have exited. Run: vibew deploy logs"
	}

	// Check for restarting containers (crash loop).
	if strings.Contains(lower, "restarting") {
		return health.CategoryContainerUnhealthy,
			"Container is restarting (possible crash loop). Run: vibew deploy logs"
	}

	// Check for unhealthy status from Docker health checks.
	if strings.Contains(lower, "unhealthy") {
		return health.CategoryContainerUnhealthy,
			"Container reports unhealthy status. Run: vibew deploy logs"
	}

	// If status contains "(failed to fetch" it means the command itself failed.
	if strings.Contains(lower, "failed to fetch") {
		return health.CategoryContainerUnhealthy,
			"Could not determine container status. Docker may not be running."
	}

	return "", ""
}

// detectTLSError looks for TLS-related error patterns in the Caddy/sidecar logs.
func detectTLSError(logs string) string {
	if logs == "" {
		return ""
	}

	lower := strings.ToLower(logs)

	tlsPatterns := []struct {
		pattern string
		detail  string
	}{
		{"acme challenge failed", "ACME challenge failed. Verify that port 80/443 is reachable and DNS points to this server."},
		{"tls handshake error", "TLS handshake error detected. Check certificate configuration and domain settings."},
		{"no certificates available", "No TLS certificates available. ACME provisioning may still be in progress or has failed."},
		{"unable to obtain certificate", "Unable to obtain TLS certificate. Check domain DNS records and firewall rules."},
		{"dns problem", "DNS problem during certificate provisioning. Verify DNS records point to this server."},
		{"certificate", "Certificate error detected. Check TLS configuration and ACME provider settings."},
	}

	for _, p := range tlsPatterns {
		if strings.Contains(lower, p.pattern) {
			return p.detail
		}
	}

	return ""
}

// detectUpstreamUnreachable probes the upstream app port directly from the
// remote host to determine if the sidecar is running but the upstream is down.
func (s *Service) detectUpstreamUnreachable(ctx context.Context, remoteDir string, port int, tlsEnabled bool) string {
	// Check if the sidecar itself is listening on its port. If it is not,
	// the issue is with the sidecar, not the upstream. We check by looking
	// for "Up" in the vibewarden container status.
	statusCmd := fmt.Sprintf("cd %s && docker compose ps vibewarden --format '{{.Status}}' 2>/dev/null", remoteDir)
	sidecarStatus, _ := s.executor.Run(ctx, statusCmd)

	if !strings.Contains(strings.ToLower(sidecarStatus), "up") {
		// Sidecar itself is not running — this is a container issue, not upstream.
		return ""
	}

	// The sidecar is running. Try to hit the health endpoint directly on localhost.
	// If we get a 502/503 or connection reset, the upstream is unreachable.
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	probeCmd := fmt.Sprintf("curl -sk -o /dev/null -w '%%{http_code}' %s://localhost:%d/_vibewarden/health 2>/dev/null || echo 000", scheme, port)
	httpCode, _ := s.executor.Run(ctx, probeCmd)
	httpCode = strings.TrimSpace(httpCode)

	switch httpCode {
	case "502", "503":
		return "Sidecar is running but upstream application returned " + httpCode + ". Check that your app is running and listening on the configured port."
	case "000":
		// curl failed entirely — sidecar port not open yet.
		return ""
	default:
		return ""
	}
}
