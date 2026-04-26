package ops

import "github.com/fatih/color"

// StatusState is the tri-state rendering tag for a component status row.
// It replaces the legacy boolean Healthy field on ComponentStatus.
type StatusState int

const (
	// StatusOK indicates the component is enabled and its health check passed,
	// or that it is always-on infrastructure (e.g. the proxy).
	StatusOK StatusState = iota
	// StatusOFF indicates the component is disabled in config. The probe is
	// suppressed — no HTTP call is made when a component is OFF.
	StatusOFF
	// StatusFAIL indicates the component is enabled but its health check failed.
	StatusFAIL
)

// String returns the canonical plain-text label for the state. This is the
// value emitted to non-TTY writers and in --no-color mode.
func (s StatusState) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusOFF:
		return "OFF"
	case StatusFAIL:
		return "FAIL"
	default:
		return "?"
	}
}

// coloredLabel returns the ANSI-coloured label for TTY output. The
// github.com/fatih/color library (already vendored) handles TTY detection
// internally and strips ANSI codes when writing to non-TTY writers or when
// NO_COLOR is set.
//
// Padding is applied to the plain label so the column width is fixed at 4
// characters (the width of "FAIL") before colour codes inflate len().
func (s StatusState) coloredLabel() string {
	label := s.String()
	// Pad the plain label to 4 characters before applying colour, so the
	// visual column width is consistent regardless of state.
	for len(label) < 4 {
		label += " "
	}
	switch s {
	case StatusOK:
		return color.New(color.FgGreen).Sprint(label)
	case StatusOFF:
		return color.New(color.FgHiBlack).Sprint(label) // dim/grey
	case StatusFAIL:
		return color.New(color.FgRed).Sprint(label)
	default:
		return label
	}
}
