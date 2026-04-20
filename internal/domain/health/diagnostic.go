package health

import "fmt"

// FailureCategory classifies why a deploy health check failed.
// This enables the CLI to provide actionable diagnostic output rather than
// a generic "health check timed out" message.
type FailureCategory string

const (
	// CategoryContainerUnhealthy means the container exited or is in an
	// unhealthy state (e.g. crash loop, OOM-killed).
	CategoryContainerUnhealthy FailureCategory = "container_unhealthy"

	// CategoryTLSError means TLS handshake or certificate provisioning failed
	// (e.g. ACME challenge failure, expired cert, misconfigured domain).
	CategoryTLSError FailureCategory = "tls_error"

	// CategoryUpstreamUnreachable means the sidecar started but the upstream
	// application is not responding (connection refused, timeout to app port).
	CategoryUpstreamUnreachable FailureCategory = "upstream_unreachable"

	// CategoryTimeout means the health check timed out without getting any
	// conclusive diagnostic signal from the other categories.
	CategoryTimeout FailureCategory = "timeout"

	// CategoryUnknown means the failure could not be classified into any
	// known category.
	CategoryUnknown FailureCategory = "unknown"
)

// String returns the string representation of the category.
func (c FailureCategory) String() string {
	return string(c)
}

// Diagnostic is an immutable value object that captures the result of a
// post-failure diagnostic analysis on a deploy health check. It contains the
// classified failure category plus supporting evidence (container status, logs,
// error details) that help the operator understand what went wrong.
type Diagnostic struct {
	category        FailureCategory
	containerStatus string
	caddyLogs       string
	detail          string
}

// NewDiagnostic creates a Diagnostic value object.
// category must be a valid FailureCategory constant.
func NewDiagnostic(category FailureCategory, containerStatus, caddyLogs, detail string) Diagnostic {
	return Diagnostic{
		category:        category,
		containerStatus: containerStatus,
		caddyLogs:       caddyLogs,
		detail:          detail,
	}
}

// Category returns the classified failure category.
func (d Diagnostic) Category() FailureCategory {
	return d.category
}

// ContainerStatus returns the raw container status output (e.g. from
// "docker compose ps") captured during diagnosis.
func (d Diagnostic) ContainerStatus() string {
	return d.containerStatus
}

// CaddyLogs returns the last N lines of Caddy/sidecar logs captured during
// diagnosis.
func (d Diagnostic) CaddyLogs() string {
	return d.caddyLogs
}

// Detail returns a human-readable explanation of the failure with actionable
// guidance for the operator.
func (d Diagnostic) Detail() string {
	return d.detail
}

// Summary returns a single-line summary suitable for CLI output.
func (d Diagnostic) Summary() string {
	switch d.category {
	case CategoryContainerUnhealthy:
		return "Container is not running or unhealthy"
	case CategoryTLSError:
		return "TLS/certificate error detected"
	case CategoryUpstreamUnreachable:
		return "Upstream application is unreachable"
	case CategoryTimeout:
		return "Health check timed out without a specific failure signal"
	default:
		return "Health check failed for unknown reason"
	}
}

// String returns a multi-line diagnostic report.
func (d Diagnostic) String() string {
	return fmt.Sprintf("category: %s\nsummary: %s\ndetail: %s",
		d.category, d.Summary(), d.detail)
}
