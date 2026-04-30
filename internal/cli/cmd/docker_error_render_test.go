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
