package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewBuildCmd_RegisteredOnRoot verifies that the build subcommand is
// reachable from the root command with its expected flags.
func TestNewBuildCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	buildCmd, _, err := root.Find([]string{"build"})
	if err != nil {
		t.Fatalf("Find(build) error: %v", err)
	}
	if buildCmd == nil || buildCmd.Use != "build" {
		t.Fatal("expected 'build' subcommand to be registered on root")
	}

	if buildCmd.Flags().Lookup("no-cache") == nil {
		t.Error("expected --no-cache flag to be registered on 'build' command")
	}
	if buildCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag to be registered on 'build' command")
	}
}

// TestNewBuildCmd_Short verifies that the Short description is set.
func TestNewBuildCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	buildCmd, _, _ := root.Find([]string{"build"})
	if buildCmd.Short == "" {
		t.Error("expected non-empty Short description on 'build' command")
	}
}

// TestNewBuildCmd_Help verifies that help output contains expected content.
func TestNewBuildCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"build", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"build", "no-cache", "config", "app.image"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewBuildCmd_NoCacheFlag verifies that --no-cache is a boolean flag with
// the correct default value.
func TestNewBuildCmd_NoCacheFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	buildCmd, _, _ := root.Find([]string{"build"})

	f := buildCmd.Flags().Lookup("no-cache")
	if f == nil {
		t.Fatal("--no-cache flag not found")
	}
	if f.DefValue != "false" {
		t.Errorf("--no-cache default = %q, want %q", f.DefValue, "false")
	}
}

// TestNewBuildCmd_ConfigFlagDefault verifies that --config defaults to empty string.
func TestNewBuildCmd_ConfigFlagDefault(t *testing.T) {
	root := cmd.NewRootCmd("test")
	buildCmd, _, _ := root.Find([]string{"build"})

	f := buildCmd.Flags().Lookup("config")
	if f == nil {
		t.Fatal("--config flag not found")
	}
	if f.DefValue != "" {
		t.Errorf("--config default = %q, want empty string", f.DefValue)
	}
}
