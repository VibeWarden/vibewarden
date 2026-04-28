package healthsummary

import "testing"

// fakeComponent is a minimal ComponentState implementation for testing.
type fakeComponent struct {
	healthy bool
	str     string
}

func (f fakeComponent) Healthy() bool  { return f.healthy }
func (f fakeComponent) String() string { return f.str }

func TestAggregateStatus(t *testing.T) {
	ok := fakeComponent{healthy: true, str: "ok"}
	failing := fakeComponent{healthy: false, str: "failing"}
	unknown := fakeComponent{healthy: false, str: "unknown"}

	tests := []struct {
		name       string
		components map[string]ComponentState
		want       Status
	}{
		{
			name:       "empty map → ok",
			components: map[string]ComponentState{},
			want:       StatusOK,
		},
		{
			name:       "nil map → ok",
			components: nil,
			want:       StatusOK,
		},
		{
			name:       "single healthy component → ok",
			components: map[string]ComponentState{"sidecar": ok},
			want:       StatusOK,
		},
		{
			name:       "all healthy → ok",
			components: map[string]ComponentState{"sidecar": ok, "upstream": ok},
			want:       StatusOK,
		},
		{
			name:       "one failing → degraded",
			components: map[string]ComponentState{"sidecar": ok, "upstream": failing},
			want:       StatusDegraded,
		},
		{
			name:       "one unknown → degraded",
			components: map[string]ComponentState{"sidecar": ok, "upstream": unknown},
			want:       StatusDegraded,
		},
		{
			name:       "nil component value → degraded (defensive)",
			components: map[string]ComponentState{"sidecar": ok, "upstream": nil},
			want:       StatusDegraded,
		},
		{
			name:       "all failing → degraded",
			components: map[string]ComponentState{"sidecar": failing, "upstream": failing},
			want:       StatusDegraded,
		},
		{
			name:       "single failing → degraded",
			components: map[string]ComponentState{"upstream": failing},
			want:       StatusDegraded,
		},
		{
			name:       "three components all ok → ok",
			components: map[string]ComponentState{"a": ok, "b": ok, "c": ok},
			want:       StatusOK,
		},
		{
			name:       "three components one bad → degraded",
			components: map[string]ComponentState{"a": ok, "b": ok, "c": unknown},
			want:       StatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AggregateStatus(tt.components)
			if got != tt.want {
				t.Errorf("AggregateStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatus_Constants(t *testing.T) {
	if string(StatusOK) != "ok" {
		t.Errorf("StatusOK = %q, want %q", StatusOK, "ok")
	}
	if string(StatusDegraded) != "degraded" {
		t.Errorf("StatusDegraded = %q, want %q", StatusDegraded, "degraded")
	}
}
