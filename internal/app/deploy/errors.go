package deploy

import (
	"fmt"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

// HealthCheckError wraps ErrHealthCheck with diagnostic information that
// classifies why the health check failed. It allows the CLI layer to present
// targeted guidance instead of a generic failure message.
type HealthCheckError struct {
	// Diagnostic contains the classified failure category, container status,
	// relevant logs, and human-readable detail.
	Diagnostic health.Diagnostic
}

// Error returns a human-readable error message including the diagnostic summary.
func (e *HealthCheckError) Error() string {
	var b strings.Builder
	b.WriteString("health check failed: ")
	b.WriteString(e.Diagnostic.Summary())
	if detail := e.Diagnostic.Detail(); detail != "" {
		b.WriteString("\n  ")
		b.WriteString(detail)
	}
	return b.String()
}

// Unwrap returns ErrHealthCheck so that errors.Is(err, ErrHealthCheck) works.
func (e *HealthCheckError) Unwrap() error {
	return ErrHealthCheck
}

// FormatDiagnostic produces a multi-line human-readable report of the health
// check diagnostic suitable for CLI output. It includes the category, summary,
// detail, container status, and relevant log lines.
func FormatDiagnostic(d health.Diagnostic) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Diagnosis: %s\n", d.Summary())
	fmt.Fprintf(&b, "Category:  %s\n", d.Category())

	if detail := d.Detail(); detail != "" {
		fmt.Fprintf(&b, "Detail:    %s\n", detail)
	}

	if status := d.ContainerStatus(); status != "" {
		b.WriteString("\nContainer status:\n")
		for _, line := range strings.Split(status, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	if logs := d.CaddyLogs(); logs != "" {
		b.WriteString("\nRecent sidecar logs:\n")
		for _, line := range strings.Split(logs, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}
