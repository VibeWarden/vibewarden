package health_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

func TestFailureCategory_String(t *testing.T) {
	tests := []struct {
		category health.FailureCategory
		want     string
	}{
		{health.CategoryContainerUnhealthy, "container_unhealthy"},
		{health.CategoryTLSError, "tls_error"},
		{health.CategoryUpstreamUnreachable, "upstream_unreachable"},
		{health.CategoryTimeout, "timeout"},
		{health.CategoryUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.category.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewDiagnostic(t *testing.T) {
	d := health.NewDiagnostic(
		health.CategoryTLSError,
		"vibewarden  running  Up 5 seconds",
		"tls handshake error: certificate not found",
		"TLS certificate provisioning failed. Check domain DNS and ACME configuration.",
	)

	if d.Category() != health.CategoryTLSError {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryTLSError)
	}
	if d.ContainerStatus() != "vibewarden  running  Up 5 seconds" {
		t.Errorf("ContainerStatus() = %q, want %q", d.ContainerStatus(), "vibewarden  running  Up 5 seconds")
	}
	if d.CaddyLogs() != "tls handshake error: certificate not found" {
		t.Errorf("CaddyLogs() = %q, want %q", d.CaddyLogs(), "tls handshake error: certificate not found")
	}
	if d.Detail() != "TLS certificate provisioning failed. Check domain DNS and ACME configuration." {
		t.Errorf("Detail() = %q, want %q", d.Detail(), "TLS certificate provisioning failed. Check domain DNS and ACME configuration.")
	}
}

func TestDiagnostic_Summary(t *testing.T) {
	tests := []struct {
		name     string
		category health.FailureCategory
		wantSub  string
	}{
		{"container_unhealthy", health.CategoryContainerUnhealthy, "not running or unhealthy"},
		{"tls_error", health.CategoryTLSError, "TLS/certificate error"},
		{"upstream_unreachable", health.CategoryUpstreamUnreachable, "Upstream application is unreachable"},
		{"timeout", health.CategoryTimeout, "timed out"},
		{"unknown", health.CategoryUnknown, "unknown reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := health.NewDiagnostic(tt.category, "", "", "")
			summary := d.Summary()
			if !strings.Contains(summary, tt.wantSub) {
				t.Errorf("Summary() = %q, want substring %q", summary, tt.wantSub)
			}
		})
	}
}

func TestDiagnostic_String(t *testing.T) {
	d := health.NewDiagnostic(
		health.CategoryContainerUnhealthy,
		"vibewarden  exited  Exited (1) 5 seconds ago",
		"error starting server",
		"Container crashed. Check logs with: vibew logs",
	)

	str := d.String()
	if !strings.Contains(str, "container_unhealthy") {
		t.Errorf("String() should contain category, got: %s", str)
	}
	if !strings.Contains(str, "not running or unhealthy") {
		t.Errorf("String() should contain summary, got: %s", str)
	}
	if !strings.Contains(str, "Container crashed") {
		t.Errorf("String() should contain detail, got: %s", str)
	}
}

func TestDiagnostic_ZeroValue(t *testing.T) {
	var d health.Diagnostic
	if d.Category() != "" {
		t.Errorf("zero value Category() = %q, want empty", d.Category())
	}
	if d.ContainerStatus() != "" {
		t.Errorf("zero value ContainerStatus() = %q, want empty", d.ContainerStatus())
	}
	if d.CaddyLogs() != "" {
		t.Errorf("zero value CaddyLogs() = %q, want empty", d.CaddyLogs())
	}
	if d.Detail() != "" {
		t.Errorf("zero value Detail() = %q, want empty", d.Detail())
	}
}
