package ops

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// makeExitErr fabricates an *exec.ExitError-equivalent for tests by running
// a command that is guaranteed to fail with a known code. It stamps
// exitErr.Stderr so the defence-in-depth fallback branch in
// formatRemoteError can be exercised directly — but see
// TestFormatRemoteError_SSHAdapterContract below for the production-shaped
// case where Stderr is empty.
func makeExitErr(t *testing.T, code int, stderr string) error {
	t.Helper()
	script := fmt.Sprintf("printf %%s %q 1>&2; exit %d", stderr, code)
	cmd := exec.Command("sh", "-c", script) //nolint:gosec // test-local, inputs are controlled
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-nil error for exit %d", code)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitErr.Stderr = []byte(stderr)
	}
	return err
}

// makeSSHAdapterErr mirrors the real ssh.Executor.Run contract: it returns
// the merged stdout+stderr as the output string and wraps the cmd error in
// "ssh exit: %w", leaving *exec.ExitError.Stderr empty. This is the shape
// formatRemoteError actually sees in production.
func makeSSHAdapterErr(t *testing.T, code int, mergedOutput string) (string, error) {
	t.Helper()
	// Run a real process that produces the given exit code so we get a
	// genuine *exec.ExitError (with Stderr unset, as cmd.Run leaves it).
	cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)) //nolint:gosec
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatalf("expected non-nil error for exit %d", code)
	}
	// Sanity: the real adapter pattern leaves Stderr empty.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if len(exitErr.Stderr) != 0 {
			t.Fatalf("precondition: *exec.ExitError.Stderr must be empty to mirror ssh adapter, got %q", exitErr.Stderr)
		}
	}
	return mergedOutput, fmt.Errorf("ssh exit: %w", runErr)
}

func TestFormatRemoteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		output        string
		cmdName       string
		wantContains  []string
		wantExclusive []string // substrings that MUST NOT appear
		wantEmpty     bool
	}{
		{
			name:      "nil error → empty string",
			err:       nil,
			output:    "",
			cmdName:   "docker compose",
			wantEmpty: true,
		},
		{
			name:          "exit 127 with stderr → hint + code",
			err:           makeExitErr(t, 127, "docker: command not found"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 127", "docker: command not found", "not installed on remote"},
			wantExclusive: []string{"2>/dev/null", "||", "\n", "ssh exit"},
		},
		{
			name:          "exit 126 with permission denied",
			err:           makeExitErr(t, 126, "permission denied"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 126", "permission denied", "not executable"},
			wantExclusive: []string{"2>/dev/null", "||"},
		},
		{
			name:          "exit 1 with multi-line stderr → only first non-empty line",
			err:           makeExitErr(t, 1, "\n\nfirst line of error\nsecond line of error"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "first line of error"},
			wantExclusive: []string{"second line of error", "\n", "2>/dev/null"},
		},
		{
			name:          "exit 1 with literal 2>/dev/null in stderr stripped",
			err:           makeExitErr(t, 1, "command failed 2>/dev/null || docker-compose ps"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1"},
			wantExclusive: []string{"2>/dev/null", "||", "docker-compose"},
		},
		{
			name:          "exit 255 → ssh hint",
			err:           makeExitErr(t, 255, "ssh: connect to host: timed out"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 255", "ssh connection failed"},
			wantExclusive: []string{"2>/dev/null", "||"},
		},
		{
			name:          "empty stderr, exit 1 → falls back to default hint",
			err:           makeExitErr(t, 1, ""),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "check remote docker compose installation"},
			wantExclusive: []string{"2>/dev/null", "||", "\n"},
		},
		{
			name:          "non-ExitError plain error → rendered as hint only",
			err:           errors.New("dial timeout"),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"dial timeout"},
			wantExclusive: []string{"exit", "2>/dev/null", "||", "ssh exit"},
		},
		{
			name:          "non-ExitError with ssh exit wrapper stripped",
			err:           fmt.Errorf("ssh exit: %w", errors.New("connection refused")),
			output:        "",
			cmdName:       "docker compose",
			wantContains:  []string{"connection refused"},
			wantExclusive: []string{"ssh exit", "2>/dev/null", "||"},
		},
		{
			name:          "output arg wins over empty ExitError.Stderr",
			err:           makeExitErr(t, 127, ""),
			output:        "docker: command not found\nExtra noise",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 127", "docker: command not found", "not installed on remote"},
			wantExclusive: []string{"Extra noise", "\n", "2>/dev/null"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatRemoteError(tt.err, tt.output, tt.cmdName)

			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty string, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected non-empty output for %v", tt.err)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("output must be single line, got %q", got)
			}
			if len(got) > maxRemoteErrorLen {
				t.Errorf("output exceeds %d chars: %q (len=%d)", maxRemoteErrorLen, got, len(got))
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output %q missing %q", got, want)
				}
			}
			for _, leak := range tt.wantExclusive {
				if strings.Contains(got, leak) {
					t.Errorf("output %q must not contain %q", got, leak)
				}
			}
		})
	}
}

func TestFormatRemoteError_NoRawShellLeaks(t *testing.T) {
	t.Parallel()

	// Feed the worst-case: an error string that contains every historical
	// leak fragment. The output must contain NONE of them.
	raw := errors.New("ssh docker compose ps --format json 2>/dev/null || docker-compose ps 2>/dev/null: exit status 127")
	got := formatRemoteError(raw, "", "docker compose")

	for _, leak := range []string{"2>/dev/null", "||", "ssh exit"} {
		if strings.Contains(got, leak) {
			t.Errorf("formatted output %q must not contain leak %q", got, leak)
		}
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("output must be single line, got %q", got)
	}
}

// TestFormatRemoteError_SSHAdapterContract is the regression test for the
// blocker surfaced on PR #1060: in production the ssh adapter returns
// merged stdout+stderr as the first return value and wraps the cmd error
// with "ssh exit: %w". *exec.ExitError.Stderr is NEVER populated because
// the adapter calls cmd.Run() with a shared buffer (not cmd.Output()).
//
// Before the fix, the real stderr line was discarded and the user only
// saw `exit <code>: <hint>`. This test feeds the adapter-shaped inputs
// and asserts the real stderr reaches the user-facing string.
func TestFormatRemoteError_SSHAdapterContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		exitCode      int
		mergedOutput  string
		cmdName       string
		wantContains  []string
		wantExclusive []string
	}{
		{
			name:          "docker missing on remote — real stderr surfaces",
			exitCode:      127,
			mergedOutput:  "bash: docker: command not found\n",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 127", "docker: command not found", "not installed on remote"},
			wantExclusive: []string{"ssh exit", "2>/dev/null", "||"},
		},
		{
			name:          "permission denied on remote",
			exitCode:      126,
			mergedOutput:  "bash: /usr/local/bin/docker: Permission denied\n",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 126", "Permission denied", "not executable"},
			wantExclusive: []string{"ssh exit"},
		},
		{
			name:          "compose error with multi-line output — only first line leaks",
			exitCode:      1,
			mergedOutput:  "no configuration file provided: not found\nstack trace line 1\nstack trace line 2\n",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "no configuration file provided"},
			wantExclusive: []string{"stack trace", "ssh exit"},
		},
		{
			name:          "empty merged output — falls back to default hint",
			exitCode:      1,
			mergedOutput:  "",
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "check remote docker compose installation"},
			wantExclusive: []string{"ssh exit"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			output, err := makeSSHAdapterErr(t, tt.exitCode, tt.mergedOutput)
			got := formatRemoteError(err, output, tt.cmdName)
			if got == "" {
				t.Fatalf("expected non-empty output for exit=%d output=%q", tt.exitCode, tt.mergedOutput)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("output must be single line, got %q", got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output %q missing %q", got, want)
				}
			}
			for _, leak := range tt.wantExclusive {
				if strings.Contains(got, leak) {
					t.Errorf("output %q must not contain %q", got, leak)
				}
			}
		})
	}
}
