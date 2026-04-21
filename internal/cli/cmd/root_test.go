package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestRootCmd_Deploy asserts that `vibew deploy` is no longer a registered
// command after ADR-086's sunset was accelerated (#1063). Invoking it must
// surface cobra's default `unknown command "deploy"` error, which the
// top-level Execute() maps to a non-zero exit code.
//
// This replaces the previous polite deprecation-stub contract asserted by
// the deleted TestDeployCmd_EmitsDeprecationAndExits2. Only the
// `unknown command "deploy"` substring is asserted so a future cobra
// upgrade that tweaks wording around that token does not break the test.
func TestRootCmd_Deploy(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"bare deploy", []string{"deploy"}},
		{"deploy with flag", []string{"deploy", "--target", "ssh://user@host"}},
		{"deploy with subcommand", []string{"deploy", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			root := NewRootCmd("test")
			root.SetErr(&stderr)
			root.SetOut(io.Discard)
			root.SetArgs(tt.args)

			err := root.Execute()
			if err == nil {
				t.Fatalf("Execute(%v) returned nil, want cobra unknown-command error", tt.args)
			}
			if !strings.Contains(err.Error(), `unknown command "deploy"`) {
				t.Errorf("Execute(%v) error = %q, want to contain `unknown command \"deploy\"`",
					tt.args, err.Error())
			}
		})
	}
}
