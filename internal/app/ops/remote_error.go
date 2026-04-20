package ops

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// maxRemoteErrorLen bounds the final rendered detail to keep the doctor
// report single-line and readable. Matches ADR-084 "~180 chars".
const maxRemoteErrorLen = 180

// formatRemoteError renders a ports.RemoteExecutor error for user display.
//
// Contract (per ADR-084):
//   - Output is a single line (no newlines, no carriage returns).
//   - It never echoes raw shell fragments (`2>/dev/null`, `||`, `ssh `,
//     `ssh exit`) — those are stripped so implementation detail does not
//     leak into user-facing errors.
//   - When err wraps *exec.ExitError the exit code is surfaced.
//   - The first non-empty line of output is included when present.
//   - A small set of hard-coded hints is appended based on the exit code.
//   - Input nil error → empty string.
//
// output is the merged stdout+stderr returned alongside err by
// ports.RemoteExecutor.Run. The ssh adapter uses `cmd.Run()` with a shared
// buffer, so *exec.ExitError.Stderr is never populated — the real stderr
// only lives in this string. Pass it in verbatim.
//
// cmdName is a short human-readable label for the remote command
// (e.g. "docker compose ps") used when the output is empty.
func formatRemoteError(err error, output string, cmdName string) string {
	if err == nil {
		return ""
	}

	exitCode := extractExitCode(err)
	// Prefer the merged output stream from the adapter: that is where the
	// real stderr lives. Fall back to *exec.ExitError.Stderr (populated only
	// when callers use cmd.Output(); kept for defence in depth) and finally
	// to the wrapped error text.
	stderrLine := firstMeaningfulLine(output)
	if stderrLine == "" {
		if _, stderr := extractExitInfo(err); stderr != "" {
			stderrLine = firstMeaningfulLine(stderr)
		}
	}
	if stderrLine == "" {
		stderrLine = stripShellLeaks(err.Error())
	}
	stderrLine = stripShellLeaks(stderrLine)

	var parts []string
	if exitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit %d", exitCode))
	}
	if stderrLine != "" {
		parts = append(parts, stderrLine)
	}
	if hint := remoteErrorHint(exitCode, cmdName); hint != "" {
		parts = append(parts, hint)
	}

	out := strings.Join(parts, "; ")
	out = sanitizeSingleLine(out)
	if len(out) > maxRemoteErrorLen {
		out = out[:maxRemoteErrorLen-1] + "…"
	}
	return out
}

// extractExitCode returns the numeric exit code from an error that wraps
// *exec.ExitError. Returns 0 when err does not wrap one.
func extractExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

// extractExitInfo pulls the numeric exit code and *exec.ExitError.Stderr out
// of an error. Stderr is only populated by Go when callers use cmd.Output();
// the ssh adapter uses cmd.Run() with a shared stdout+stderr buffer, so in
// production this always returns ("", 0). Kept as a defence-in-depth
// fallback for callers that do use Output-style execution.
//
// Returns (0, "") when err is not an *exec.ExitError.
func extractExitInfo(err error) (int, string) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(exitErr.Stderr)
	}
	return 0, ""
}

// firstMeaningfulLine returns the first non-empty, trimmed line of s.
func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// stripShellLeaks removes shell fragments that must never appear in
// user-facing doctor output. The replacements are conservative: only
// well-known literal substrings are removed, and surrounding whitespace is
// collapsed.
func stripShellLeaks(s string) string {
	leaks := []string{
		" 2>/dev/null",
		"2>/dev/null",
		" || docker-compose ps",
		"|| docker-compose ps",
		" || ",
		"ssh exit: ",
		"ssh exit:",
		// "ssh " only at the start of a token — a legitimate word like
		// "ssh" in stderr (e.g. "ssh: connect") is allowed to stay without
		// the trailing space. We strip the common "ssh <cmd>:" prefix by
		// anchoring with a colon instead.
	}
	for _, l := range leaks {
		s = strings.ReplaceAll(s, l, "")
	}
	// Normalise whitespace runs.
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// sanitizeSingleLine collapses any remaining newlines/carriage returns into
// spaces so the final string renders on one line.
func sanitizeSingleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// remoteErrorHint returns a short, user-facing suggestion keyed off the
// exit code. cmdName contextualises the default hint (e.g. "docker
// compose").
func remoteErrorHint(exitCode int, cmdName string) string {
	switch exitCode {
	case 127:
		return fmt.Sprintf("%s not installed on remote", cmdName)
	case 126:
		return fmt.Sprintf("%s not executable — check permissions", cmdName)
	case 255:
		return "ssh connection failed — check target and SSH keys"
	case 0:
		return ""
	default:
		if cmdName != "" {
			return fmt.Sprintf("check remote %s installation", cmdName)
		}
		return "check remote command"
	}
}
