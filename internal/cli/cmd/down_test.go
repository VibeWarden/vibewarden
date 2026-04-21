package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewDownCmd_RegisteredOnRoot verifies that the down subcommand is reachable
// from the root command and that its expected flags are registered.
func TestNewDownCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	downCmd, _, err := root.Find([]string{"down"})
	if err != nil {
		t.Fatalf("Find(down) error: %v", err)
	}
	if downCmd == nil || downCmd.Use != "down" {
		t.Fatal("expected 'down' subcommand to be registered on root")
	}

	for _, flag := range []string{"volumes", "remove-orphans", "yes"} {
		if downCmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s flag to be registered on 'down' command", flag)
		}
	}
}

// TestNewDownCmd_VolumesShortFlag verifies that -v is the shortform of --volumes.
func TestNewDownCmd_VolumesShortFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	downCmd, _, _ := root.Find([]string{"down"})

	volFlag := downCmd.Flags().Lookup("volumes")
	if volFlag == nil {
		t.Fatal("--volumes flag not registered")
	}
	if volFlag.Shorthand != "v" {
		t.Errorf("expected -v shorthand for --volumes, got %q", volFlag.Shorthand)
	}
}

// TestNewDownCmd_Help verifies that help output covers the documented flags,
// the relationship to 'vibew dev', and the 'vibew logs -f' hint.
func TestNewDownCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"down", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"down", "vibew dev", "vibew logs", "--volumes", "--yes", "Idempotent"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewDownCmd_Short verifies the Short description is set.
func TestNewDownCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	downCmd, _, _ := root.Find([]string{"down"})
	if downCmd.Short == "" {
		t.Error("expected non-empty Short description on 'down' command")
	}
}
