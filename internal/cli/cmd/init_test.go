package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestNewInitCmd_PrintsPlaceholder(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("init command should not error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "vibewarden wrap") {
		t.Errorf("expected output to mention 'vibewarden wrap', got: %s", output)
	}
	if !strings.Contains(output, "coming soon") {
		t.Errorf("expected output to mention 'coming soon', got: %s", output)
	}
}

func TestNewInitCmd_IgnoresArgs(t *testing.T) {
	// Placeholder should not error on args (for discoverability).
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", "some-directory", "--upstream", "3000"})

	err := root.Execute()
	// Should not error, just print message.
	if err != nil {
		t.Fatalf("init command should not error even with args: %v", err)
	}
}
