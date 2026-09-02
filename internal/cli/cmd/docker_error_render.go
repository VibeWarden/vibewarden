package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// renderDockerUnavailable writes the multi-line operator-friendly error block
// to w when err wraps a *ports.DockerUnavailableError. It is a no-op when err
// does not carry that type.
//
// Format (exact):
//
//	Error: Docker is unavailable.
//
//	  Ensure Docker Desktop is running and your user has access to
//	  the socket.
//
//	  On macOS:  open Docker Desktop
//	  On Linux:  sudo usermod -aG docker $USER && newgrp docker
//
//	Underlying error:
//	  <original docker stderr, each line indented with two spaces>
//
// Docker stderr is untrusted subprocess output, so it is passed through
// sanitizeTerminalText before it reaches w: terminal escape sequences and
// control characters cannot be used to manipulate the operator's terminal.
//
// When stderr is empty (whitespace-only after sanitisation and trim), the
// "Underlying error:" section is omitted entirely.
func renderDockerUnavailable(w io.Writer, err error) {
	var de *ports.DockerUnavailableError
	if !errors.As(err, &de) {
		return
	}

	fmt.Fprintln(w, "Error: Docker is unavailable.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  Ensure Docker Desktop is running and your user has access to")
	fmt.Fprintln(w, "  the socket.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  On macOS:  open Docker Desktop")
	fmt.Fprintln(w, "  On Linux:  sudo usermod -aG docker $USER && newgrp docker")

	trimmed := strings.TrimSpace(sanitizeTerminalText(de.Stderr))
	if trimmed == "" {
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Underlying error:")
	for _, line := range strings.Split(trimmed, "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}
