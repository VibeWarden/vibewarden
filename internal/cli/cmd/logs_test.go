package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewLogsCmd_RegisteredOnRoot verifies that the logs subcommand is
// reachable from the root command with its expected flags registered.
func TestNewLogsCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	logsCmd, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("Find(logs) error: %v", err)
	}
	if logsCmd == nil || logsCmd.Use != "logs" {
		t.Fatal("expected 'logs' subcommand to be registered on root")
	}

	flags := []struct {
		name      string
		shorthand string
	}{
		{"follow", "f"},
		{"filter", ""},
		{"json", ""},
		{"verbose", "v"},
		{"stdin", ""},
	}

	for _, f := range flags {
		if logsCmd.Flags().Lookup(f.name) == nil {
			t.Errorf("expected --%s flag to be registered on 'logs' command", f.name)
		}
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

// TestNewLogsCmd_Help verifies that help output for logs contains expected content.
func TestNewLogsCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"logs", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"logs", "follow", "filter", "stdin"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewLogsCmd_StdinMode verifies that the logs command runs when reading
// from stdin using a pipe of empty input.
func TestNewLogsCmd_StdinMode(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	// Provide empty stdin; the command should complete without error.
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"logs", "--stdin"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error with empty stdin: %v\nstderr: %s", err, errBuf.String())
	}
}

// TestNewLogsCmd_StdinModeWithLines verifies that plain non-JSON lines from
// stdin are processed without error.
func TestNewLogsCmd_StdinModeWithLines(t *testing.T) {
	lines := "not json at all\nanother plain line\n"

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(lines))
	root.SetArgs([]string{"logs", "--stdin"})

	// The command must complete without error when receiving non-JSON lines.
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error processing non-JSON lines: %v\nstderr: %s", err, errBuf.String())
	}
}

// TestNewLogsCmd_StdinModeRawJSON verifies that --json flag does not crash
// when processing structured log lines from stdin.
func TestNewLogsCmd_StdinModeRawJSON(t *testing.T) {
	jsonLine := `{"schema_version":"v1","event_type":"proxy.started","timestamp":"2026-03-26T10:00:00Z","ai_summary":"Proxy started on :8080","payload":{}}` + "\n"

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(jsonLine))
	root.SetArgs([]string{"logs", "--stdin", "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error with raw JSON flag: %v\nstderr: %s", err, errBuf.String())
	}
}

// TestNewLogsCmd_FilterFlag verifies that the --filter flag is accepted
// and does not cause an error when processing input from stdin.
func TestNewLogsCmd_FilterFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"logs", "--stdin", "--filter", "auth"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error with --filter flag: %v", err)
	}
}
