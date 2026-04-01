package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestMigrateCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")

	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "migrate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("migrate command not registered on root")
	}
}

func TestMigrateCmd_Subcommands(t *testing.T) {
	root := cmd.NewRootCmd("test")

	for _, c := range root.Commands() {
		if c.Name() == "migrate" {
			subNames := make(map[string]bool)
			for _, sub := range c.Commands() {
				subNames[sub.Name()] = true
			}
			for _, want := range []string{"up", "down", "status"} {
				if !subNames[want] {
					t.Errorf("migrate subcommand %q not found", want)
				}
			}
			return
		}
	}
	t.Fatal("migrate command not found")
}

func TestMigrateCmd_NoDatabaseURL(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"migrate", "status", "--config", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error when no database URL configured, got nil")
	}

	if !strings.Contains(err.Error(), "database URL") && !strings.Contains(errBuf.String(), "database URL") {
		t.Errorf("expected 'database URL' in error, got err=%q stderr=%q", err.Error(), errBuf.String())
	}
}

func TestMigrateCmd_HelpOutput(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"migrate", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"up", "down", "status", "database"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q, got: %q", want, out)
		}
	}
}
