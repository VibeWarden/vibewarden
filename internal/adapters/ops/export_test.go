package ops

import (
	"bytes"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ParseDownOutputForTest exposes the unexported parseDownOutput helper so that
// tests in the _test package can exercise the parser directly.
func ParseDownOutputForTest(stderr string) ports.DownResult {
	return parseDownOutput(stderr)
}

// NewComposeAdapterForTest creates a ComposeAdapter whose stderrSink is a
// fresh bytes.Buffer. The buffer is returned so tests can assert on captured
// stderr without touching the real os.Stderr file descriptor.
func NewComposeAdapterForTest() (*ComposeAdapter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &ComposeAdapter{stderrSink: buf}, buf
}

// IsNoOpErrorForTest exposes the unexported isNoOpError helper so that
// tests in the _test package can verify the no-op error classification logic.
func IsNoOpErrorForTest(lower string) bool {
	return isNoOpError(lower)
}
