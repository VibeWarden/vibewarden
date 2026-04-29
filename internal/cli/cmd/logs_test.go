package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewLogsCmd_RegisteredOnRoot verifies that the logs subcommand is
// reachable from the root command.
func TestNewLogsCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	logsCmd, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("Find(logs) error: %v", err)
	}
	if logsCmd == nil || logsCmd.Use == "" {
		t.Fatal("expected 'logs' subcommand to be registered on root")
	}
	if !strings.HasPrefix(logsCmd.Use, "logs") {
		t.Fatalf("expected Use to start with 'logs', got %q", logsCmd.Use)
	}
}

// TestNewLogsCmd_FlagsRegistered verifies that all expected flags are
// registered on the logs subcommand.
func TestNewLogsCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}

	tests := []struct {
		flagName  string
		shorthand string
	}{
		{"tail", ""},
		{"follow", "f"},
		{"since", ""},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			f := logsCmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Errorf("expected --%s flag to be registered", tt.flagName)
				return
			}
			if tt.shorthand != "" && f.Shorthand != tt.shorthand {
				t.Errorf("--%s shorthand = %q, want %q", tt.flagName, f.Shorthand, tt.shorthand)
			}
		})
	}
}

// TestNewLogsCmd_Short verifies the Short description is non-empty.
func TestNewLogsCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}
	if logsCmd.Short == "" {
		t.Error("expected non-empty Short description on 'logs' command")
	}
}

// TestNewLogsCmd_HelpText verifies that help output contains the key flags
// and usage tips.
func TestNewLogsCmd_HelpText(t *testing.T) {
	root := cmd.NewRootCmd("test")

	// Capture help via SetHelpFunc or by running --help.
	logsCmd, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("Find(logs): %v", err)
	}
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}

	var out strings.Builder
	logsCmd.SetOut(&out)
	logsCmd.SetErr(&out)

	// Trigger help output.
	logsCmd.HelpFunc()(logsCmd, []string{})
	helpText := out.String()

	wantContains := []string{
		"tail",
		"follow",
		"since",
		"vibewarden",
		"vibew dev",
	}
	for _, want := range wantContains {
		if !strings.Contains(helpText, want) {
			t.Errorf("help text missing %q\n---help---\n%s", want, helpText)
		}
	}
}

// TestNewLogsCmd_OldFlagsRemoved verifies that the old pretty-printer flags
// (--filter, --json, --verbose, --stdin) are NOT registered on the new command.
func TestNewLogsCmd_OldFlagsRemoved(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}

	removedFlags := []string{"filter", "json", "verbose", "stdin"}
	for _, f := range removedFlags {
		if logsCmd.Flags().Lookup(f) != nil {
			t.Errorf("old flag --%s should NOT be registered on the new logs command", f)
		}
	}
}

// TestNewLogsCmd_TailDefault verifies that --tail defaults to 100.
func TestNewLogsCmd_TailDefault(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}

	f := logsCmd.Flags().Lookup("tail")
	if f == nil {
		t.Fatal("--tail flag not found")
	}
	if f.DefValue != "100" {
		t.Errorf("--tail default = %q, want %q", f.DefValue, "100")
	}
}

// TestNewLogsCmd_ArbitraryArgs verifies that the command accepts variadic
// positional arguments (no cobra.MaximumNArgs constraint).
func TestNewLogsCmd_ArbitraryArgs(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, _ := root.Find([]string{"logs"})
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}

	// cobra.ArbitraryArgs allows any number of positional args. We verify
	// that Args validation accepts multiple services without error.
	err := logsCmd.ValidateArgs([]string{"vibewarden", "app", "kratos"})
	if err != nil {
		t.Errorf("ValidateArgs([vibewarden, app, kratos]) unexpected error: %v", err)
	}

	err = logsCmd.ValidateArgs([]string{})
	if err != nil {
		t.Errorf("ValidateArgs([]) unexpected error: %v", err)
	}
}
