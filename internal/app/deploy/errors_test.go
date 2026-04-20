package deploy

import (
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

func TestHealthCheckError_Error(t *testing.T) {
	diag := health.NewDiagnostic(health.CategoryTLSError, "", "", "TLS certificate expired")
	err := &HealthCheckError{Diagnostic: diag}

	got := err.Error()
	if !strings.Contains(got, "health check failed") {
		t.Errorf("Error() = %q, want to contain 'health check failed'", got)
	}
	if !strings.Contains(got, "TLS certificate expired") {
		t.Errorf("Error() = %q, want to contain detail", got)
	}
}

func TestHealthCheckError_Unwrap(t *testing.T) {
	err := &HealthCheckError{
		Diagnostic: health.NewDiagnostic(health.CategoryUnknown, "", "", ""),
	}
	if !errors.Is(err, ErrHealthCheck) {
		t.Error("errors.Is(HealthCheckError, ErrHealthCheck) = false, want true")
	}
}

func TestFormatDiagnostic(t *testing.T) {
	tests := []struct {
		name       string
		diag       health.Diagnostic
		wantParts  []string
		wantAbsent []string
	}{
		{
			name:      "full diagnostic",
			diag:      health.NewDiagnostic(health.CategoryTLSError, "ACME rate limit", "app (unhealthy)", "error obtaining cert"),
			wantParts: []string{"tls_error", "ACME rate limit", "app (unhealthy)", "error obtaining cert"},
		},
		{
			name:       "minimal diagnostic",
			diag:       health.NewDiagnostic(health.CategoryUnknown, "", "", ""),
			wantParts:  []string{"unknown"},
			wantAbsent: []string{"Detail:", "Container status:", "Recent sidecar logs:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDiagnostic(tt.diag)
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("FormatDiagnostic() missing %q in:\n%s", part, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("FormatDiagnostic() should not contain %q in:\n%s", absent, got)
				}
			}
		})
	}
}
