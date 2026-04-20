package ops

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// makeExitErr fabricates an *exec.ExitError-equivalent for tests by
// running a command that is guaranteed to fail with a known code. For
// pure, table-driven unit tests we wrap a synthetic exit error via
// exec.Command + an unknown binary, which is portable enough for the
// exit-code extraction path. When the real exit code cannot be forced
// the test uses a raw error to exercise the "not an ExitError" branch.
func makeExitErr(t *testing.T, code int, stderr string) error {
	t.Helper()
	// We cannot easily construct *exec.ExitError with a custom code
	// without spawning a process. Use "sh -c exit <code>" which is
	// available on macOS+Linux (CI platforms). stderr is stamped via
	// a printf to stderr before exit.
	script := fmt.Sprintf("printf %%s %q 1>&2; exit %d", stderr, code)
	cmd := exec.Command("sh", "-c", script) //nolint:gosec // test-local, inputs are controlled
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-nil error for exit %d", code)
	}
	// Attach stderr into *exec.ExitError.Stderr for extractExitInfo to find.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitErr.Stderr = []byte(stderr)
	}
	return err
}

func TestFormatRemoteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		cmdName       string
		wantContains  []string
		wantExclusive []string // substrings that MUST NOT appear
		wantEmpty     bool
	}{
		{
			name:      "nil error → empty string",
			err:       nil,
			cmdName:   "docker compose",
			wantEmpty: true,
		},
		{
			name:          "exit 127 with stderr → hint + code",
			err:           makeExitErr(t, 127, "docker: command not found"),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 127", "docker: command not found", "not installed on remote"},
			wantExclusive: []string{"2>/dev/null", "||", "\n", "ssh exit"},
		},
		{
			name:          "exit 126 with permission denied",
			err:           makeExitErr(t, 126, "permission denied"),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 126", "permission denied", "not executable"},
			wantExclusive: []string{"2>/dev/null", "||"},
		},
		{
			name:          "exit 1 with multi-line stderr → only first non-empty line",
			err:           makeExitErr(t, 1, "\n\nfirst line of error\nsecond line of error"),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "first line of error"},
			wantExclusive: []string{"second line of error", "\n", "2>/dev/null"},
		},
		{
			name:          "exit 1 with literal 2>/dev/null in stderr stripped",
			err:           makeExitErr(t, 1, "command failed 2>/dev/null || docker-compose ps"),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1"},
			wantExclusive: []string{"2>/dev/null", "||", "docker-compose"},
		},
		{
			name:          "exit 255 → ssh hint",
			err:           makeExitErr(t, 255, "ssh: connect to host: timed out"),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 255", "ssh connection failed"},
			wantExclusive: []string{"2>/dev/null", "||"},
		},
		{
			name:          "empty stderr, exit 1 → falls back to default hint",
			err:           makeExitErr(t, 1, ""),
			cmdName:       "docker compose",
			wantContains:  []string{"exit 1", "check remote docker compose installation"},
			wantExclusive: []string{"2>/dev/null", "||", "\n"},
		},
		{
			name:          "non-ExitError plain error → rendered as hint only",
			err:           errors.New("dial timeout"),
			cmdName:       "docker compose",
			wantContains:  []string{"dial timeout"},
			wantExclusive: []string{"exit", "2>/dev/null", "||", "ssh exit"},
		},
		{
			name:          "non-ExitError with ssh exit wrapper stripped",
			err:           fmt.Errorf("ssh exit: %w", errors.New("connection refused")),
			cmdName:       "docker compose",
			wantContains:  []string{"connection refused"},
			wantExclusive: []string{"ssh exit", "2>/dev/null", "||"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatRemoteError(tt.err, tt.cmdName)

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
	got := formatRemoteError(raw, "docker compose")

	for _, leak := range []string{"2>/dev/null", "||", "ssh exit"} {
		if strings.Contains(got, leak) {
			t.Errorf("formatted output %q must not contain leak %q", got, leak)
		}
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("output must be single line, got %q", got)
	}
}
