package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewDevCmd_FlagValidation_RebuildWatch verifies that combining --rebuild
// and --watch is rejected before any I/O takes place.
func TestNewDevCmd_FlagValidation_RebuildWatch(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"dev", "--rebuild", "--watch"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for --rebuild --watch combination, got nil")
	}
	if !strings.Contains(err.Error(), "--rebuild cannot be combined with --watch") {
		t.Errorf("error should mention incompatible flags, got: %v", err)
	}
}

// TestNewDevCmd_FlagValidation_VolumesWithoutRebuild verifies that --volumes
// without --rebuild is rejected before any I/O takes place.
func TestNewDevCmd_FlagValidation_VolumesWithoutRebuild(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"dev", "--volumes"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for --volumes without --rebuild, got nil")
	}
	if !strings.Contains(err.Error(), "--volumes requires --rebuild") {
		t.Errorf("error should mention --volumes requires --rebuild, got: %v", err)
	}
}

// TestNewDevCmd_RebuildFlagsRegistered verifies that --rebuild and --volumes
// flags are registered on the dev subcommand.
func TestNewDevCmd_RebuildFlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")

	devCmd, _, err := root.Find([]string{"dev"})
	if err != nil {
		t.Fatalf("Find(dev) error: %v", err)
	}
	if devCmd == nil || devCmd.Use != "dev" {
		t.Fatal("expected 'dev' subcommand to be registered on root")
	}

	for _, flag := range []string{"rebuild", "volumes"} {
		if devCmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s flag to be registered on 'dev' command", flag)
		}
	}
}
