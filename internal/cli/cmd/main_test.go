package cmd_test

import (
	"os"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestMain forces non-interactive mode for all CLI command tests.
// Tests that want to exercise interactive prompts must override cmd.IsTTY locally
// and restore it via t.Cleanup.
func TestMain(m *testing.M) {
	// In test runs stdin is never a real TTY; force non-interactive mode so that
	// tests run non-interactively without prompting for user input.
	cmd.IsTTY = func(*os.File) bool { return false }
	os.Exit(m.Run())
}
