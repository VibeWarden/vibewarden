package site

import "testing"

func TestStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{"healthy", StatusHealthy, "healthy"},
		{"error", StatusError, "error"},
		{"degraded", StatusDegraded, "degraded"},
		{"unknown value", Status(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
