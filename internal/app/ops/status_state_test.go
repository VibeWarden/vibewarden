package ops

import (
	"strings"
	"testing"
)

func TestStatusState_String(t *testing.T) {
	tests := []struct {
		name  string
		state StatusState
		want  string
	}{
		{"OK", StatusOK, "OK"},
		{"OFF", StatusOFF, "OFF"},
		{"FAIL", StatusFAIL, "FAIL"},
		{"unknown value", StatusState(99), "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("StatusState(%d).String() = %q, want %q", int(tt.state), got, tt.want)
			}
		})
	}
}

// TestStatusState_ColoredLabel_PlainText verifies that the padded plain
// label (no ANSI escapes) is contained within the coloredLabel output.
// fatih/color strips ANSI when the writer is not a TTY; since this test
// runs in a non-TTY test environment the result is plain text.
func TestStatusState_ColoredLabel_PlainText(t *testing.T) {
	tests := []struct {
		name      string
		state     StatusState
		wantLabel string
		wantLen   int // expected visual (rune) width — always 4
	}{
		{"OK padded to 4", StatusOK, "OK  ", 4},
		{"OFF padded to 4", StatusOFF, "OFF ", 4},
		{"FAIL exact 4", StatusFAIL, "FAIL", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.coloredLabel()
			// In a non-TTY test runner fatih/color returns plain text.
			if !strings.Contains(got, strings.TrimRight(tt.wantLabel, " ")) {
				t.Errorf("coloredLabel() = %q, want it to contain %q", got, strings.TrimRight(tt.wantLabel, " "))
			}
		})
	}
}
