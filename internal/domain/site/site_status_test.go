package site

import "testing"

func TestSiteStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status SiteStatus
		want   string
	}{
		{"healthy", StatusHealthy, "healthy"},
		{"error", StatusError, "error"},
		{"degraded", StatusDegraded, "degraded"},
		{"unknown value", SiteStatus(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("SiteStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
