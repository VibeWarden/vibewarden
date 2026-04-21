package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestDeployCmd_EmitsDeprecationAndExits2 is the acceptance test required by
// ADR-086 §"Stub command" and PM spec §3. It drives the stub cobra command
// in-process, captures stderr, and asserts:
//
//  1. the exact deprecation message is written to stderr,
//  2. the process would exit with code 2 (via the swappable exitFunc hook),
//  3. neither flag parsing nor subcommand dispatch produces a different
//     message — `--target`, `--dry-run`, and positionals like `status` /
//     `logs` are swallowed by DisableFlagParsing.
func TestDeployCmd_EmitsDeprecationAndExits2(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"with --target flag", []string{"--target", "ssh://user@host"}},
		{"with --dry-run flag", []string{"--dry-run"}},
		{"with status positional", []string{"status"}},
		{"with logs positional and --follow", []string{"logs", "--follow"}},
		{"with arbitrary unknown flag", []string{"--zzz-not-a-flag"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			var gotExit int
			restore := stubExitFunc(&gotExit)
			defer restore()

			cmd := NewDeployCmd()
			cmd.SetErr(&stderr)
			cmd.SetArgs(tt.args)

			runStubWithExitRecovery(t, cmd)

			if !strings.Contains(stderr.String(), deprecationMessage) {
				t.Errorf("stderr missing deprecation message.\nwant:\n%s\n\ngot:\n%s",
					deprecationMessage, stderr.String())
			}

			if gotExit != 2 {
				t.Errorf("exit code = %d, want 2", gotExit)
			}
		})
	}
}

// runStubWithExitRecovery executes cmd and recovers the exit-sentinel panic
// that stubExitFunc raises, so the test assertions below can run. Any other
// panic is re-raised.
func runStubWithExitRecovery(t *testing.T, cmd interface {
	Execute() error
},
) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(deployStubExitSentinel); !ok {
				panic(r)
			}
		}
	}()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil (stub uses Run not RunE)", err)
	}
}

// TestDeployCmd_IsHidden asserts the stub is registered as a hidden command
// so it does not appear in `vibew --help`.
func TestDeployCmd_IsHidden(t *testing.T) {
	cmd := NewDeployCmd()
	if !cmd.Hidden {
		t.Error("NewDeployCmd().Hidden = false, want true (stub must not appear in --help)")
	}
	if !cmd.DisableFlagParsing {
		t.Error("NewDeployCmd().DisableFlagParsing = false, want true (subcommands and flags must be swallowed)")
	}
}

// stubExitFunc swaps the package-level exitFunc with a recorder that writes
// the requested exit code into got. The returned closure restores the
// original. It also guards against the stub continuing execution after the
// fake exit by panicking the goroutine the same way a real os.Exit would
// halt it — except here the panic is recovered by the test harness to keep
// the assertions readable.
func stubExitFunc(got *int) func() {
	prev := exitFunc
	exitFunc = func(code int) {
		*got = code
		// Mimic os.Exit's non-return semantics so the deferred stderr write
		// in the Run closure cannot run twice across test cases. A
		// runtime.Goexit would also work; panic-recover keeps the blast
		// radius on the test goroutine.
		panic(deployStubExitSentinel{})
	}
	return func() {
		exitFunc = prev
	}
}

// deployStubExitSentinel is the recover token used by stubExitFunc to stop
// Run execution without affecting other goroutines.
type deployStubExitSentinel struct{}
