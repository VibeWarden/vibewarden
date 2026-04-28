package upstream

import "testing"

func TestNewConstructors(t *testing.T) {
	tests := []struct {
		name     string
		state    State
		wantKind Kind
		wantStr  string
		wantHlt  bool
		wantErr  string
	}{
		{
			name:     "zero value is Unknown",
			state:    State{},
			wantKind: KindUnknown,
			wantStr:  "unknown",
			wantHlt:  false,
		},
		{
			name:     "NewUnknown",
			state:    NewUnknown(),
			wantKind: KindUnknown,
			wantStr:  "unknown",
			wantHlt:  false,
		},
		{
			name:     "NewOk",
			state:    NewOk(),
			wantKind: KindOk,
			wantStr:  "ok",
			wantHlt:  true,
		},
		{
			name:     "NewDegraded",
			state:    NewDegraded(),
			wantKind: KindDegraded,
			wantStr:  "degraded",
			wantHlt:  true,
		},
		{
			name:     "NewFailing with error",
			state:    NewFailing("connection refused"),
			wantKind: KindFailing,
			wantStr:  "failing",
			wantHlt:  false,
			wantErr:  "connection refused",
		},
		{
			name:     "NewFailing with empty error",
			state:    NewFailing(""),
			wantKind: KindFailing,
			wantStr:  "failing",
			wantHlt:  false,
			wantErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state.Kind() != tt.wantKind {
				t.Errorf("Kind() = %v, want %v", tt.state.Kind(), tt.wantKind)
			}
			if tt.state.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", tt.state.String(), tt.wantStr)
			}
			if tt.state.Healthy() != tt.wantHlt {
				t.Errorf("Healthy() = %v, want %v", tt.state.Healthy(), tt.wantHlt)
			}
			if tt.state.LastError() != tt.wantErr {
				t.Errorf("LastError() = %q, want %q", tt.state.LastError(), tt.wantErr)
			}
		})
	}
}

func TestState_HealthySemantics(t *testing.T) {
	// Only Ok and Degraded are healthy; Unknown and Failing are not.
	healthy := []State{NewOk(), NewDegraded()}
	unhealthy := []State{NewUnknown(), {}, NewFailing("err")}

	for _, s := range healthy {
		if !s.Healthy() {
			t.Errorf("expected Healthy()=true for %q, got false", s.String())
		}
	}
	for _, s := range unhealthy {
		if s.Healthy() {
			t.Errorf("expected Healthy()=false for %q, got true", s.String())
		}
	}
}

func TestState_StringStability(t *testing.T) {
	// String must return the same value on repeated calls for the same receiver.
	for _, s := range []State{NewUnknown(), NewOk(), NewDegraded(), NewFailing("err")} {
		first := s.String()
		second := s.String()
		if first != second {
			t.Errorf("String() returned different values for %v: %q vs %q", s.Kind(), first, second)
		}
	}
}
