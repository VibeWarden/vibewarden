package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestRenderDockerUnavailable_Format verifies the exact multi-line output for a
// DockerUnavailableError with non-empty stderr.
func TestRenderDockerUnavailable_Format(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerSocketPermission,
		Stderr:   "permission denied while trying to connect to the Docker API socket",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	wantFragments := []string{
		"Error: Docker is unavailable.",
		"Ensure Docker Desktop is running",
		"On macOS:  open Docker Desktop",
		"On Linux:  sudo usermod -aG docker $USER",
		"Underlying error:",
		"  permission denied while trying to connect",
	}
	for _, frag := range wantFragments {
		if !strings.Contains(got, frag) {
			t.Errorf("output missing %q\ngot:\n%s", frag, got)
		}
	}
}

// TestRenderDockerUnavailable_EmptyStderr verifies that the "Underlying error:"
// section is omitted when Stderr is empty.
func TestRenderDockerUnavailable_EmptyStderr(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerDaemonNotRunning,
		Stderr:   "",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	if strings.Contains(got, "Underlying error:") {
		t.Errorf("expected 'Underlying error:' section to be absent for empty stderr; got:\n%s", got)
	}
	if !strings.Contains(got, "Error: Docker is unavailable.") {
		t.Errorf("expected header present; got:\n%s", got)
	}
}

// TestRenderDockerUnavailable_MultiLineStderr verifies that multi-line stderr is
// indented line by line with two spaces each.
func TestRenderDockerUnavailable_MultiLineStderr(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerDaemonNotRunning,
		Stderr:   "line one\nline two\nline three",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	for _, want := range []string{"  line one", "  line two", "  line three"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output; got:\n%s", want, got)
		}
	}
}

// TestRenderDockerUnavailable_TrailingWhitespaceStderr verifies that leading/
// trailing whitespace in Stderr is trimmed before rendering.
func TestRenderDockerUnavailable_TrailingWhitespaceStderr(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerDaemonNotRunning,
		Stderr:   "\n\n  Cannot connect to the Docker daemon  \n\n",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	// "Underlying error:" must appear.
	if !strings.Contains(got, "Underlying error:") {
		t.Errorf("expected 'Underlying error:' for non-empty stderr after trim; got:\n%s", got)
	}
	// The trimmed content must be indented.
	if !strings.Contains(got, "  Cannot connect to the Docker daemon") {
		t.Errorf("expected trimmed and indented stderr line; got:\n%s", got)
	}
}

// TestRenderDockerUnavailable_SanitizesEscapeSequences asserts on the literal
// bytes written to the writer: a Docker daemon that emits ANSI/VT100 escape
// sequences on stderr must not be able to drive the operator's terminal.
func TestRenderDockerUnavailable_SanitizesEscapeSequences(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerDaemonNotRunning,
		Stderr:   "\x1b[2J\x1b[H\x1b]0;pwned\x07Cannot connect\x1b[31m to the daemon\x1b[0m",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	for _, bad := range []rune{0x1B, 0x07, 0x9B} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("control U+%04X reached the writer:\n%q", bad, got)
		}
	}
	if !strings.Contains(got, "  Cannot connect to the daemon\n") {
		t.Errorf("expected sanitised stderr line; got:\n%q", got)
	}
}

// TestRenderDockerUnavailable_SanitizesSingleByteControls verifies that a lone
// control byte (not part of a multi-byte escape sequence) is stripped too,
// while tab is preserved.
func TestRenderDockerUnavailable_SanitizesSingleByteControls(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerSocketPermission,
		Stderr:   "permission\x07 denied\rHIDDEN\x08\tsocket",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	want := "  permission deniedHIDDEN\tsocket\n"
	if !strings.Contains(got, want) {
		t.Errorf("expected %q in output; got:\n%q", want, got)
	}
	for _, bad := range []rune{0x07, '\r', 0x08} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("control U+%04X reached the writer:\n%q", bad, got)
		}
	}
}

// TestRenderDockerUnavailable_EscapeOnlyStderr verifies that stderr consisting
// solely of escape sequences collapses to empty, so the "Underlying error:"
// section is omitted rather than printed with a blank body.
func TestRenderDockerUnavailable_EscapeOnlyStderr(t *testing.T) {
	e := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerDaemonNotRunning,
		Stderr:   "\x1b[31m\x1b[0m\x1b]0;title\x07\r\n",
	}

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, e)
	got := buf.String()

	if strings.Contains(got, "Underlying error:") {
		t.Errorf("expected 'Underlying error:' to be omitted for escape-only stderr; got:\n%q", got)
	}
}

// TestRenderDockerUnavailable_NonDockerError verifies that the function is a
// no-op when err does not carry *DockerUnavailableError.
func TestRenderDockerUnavailable_NonDockerError(t *testing.T) {
	var buf bytes.Buffer
	renderDockerUnavailable(&buf, errors.New("some unrelated error"))
	if buf.Len() != 0 {
		t.Errorf("expected no output for non-docker error; got %q", buf.String())
	}
}

// TestRenderDockerUnavailable_NilError verifies that a nil error does not panic.
func TestRenderDockerUnavailable_NilError(t *testing.T) {
	var buf bytes.Buffer
	renderDockerUnavailable(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil error; got %q", buf.String())
	}
}

// TestRenderDockerUnavailable_WrappedError verifies that renderDockerUnavailable
// works when the DockerUnavailableError is wrapped inside another error via %w.
func TestRenderDockerUnavailable_WrappedError(t *testing.T) {
	inner := &ports.DockerUnavailableError{
		Sentinel: ports.ErrDockerSocketPermission,
		Stderr:   "permission denied",
	}
	wrapped := fmt.Errorf("docker compose up: %w", inner)

	var buf bytes.Buffer
	renderDockerUnavailable(&buf, wrapped)
	if !strings.Contains(buf.String(), "Error: Docker is unavailable.") {
		t.Errorf("expected render for wrapped error; got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "  permission denied") {
		t.Errorf("expected stderr indented in wrapped error render; got:\n%s", buf.String())
	}
}
