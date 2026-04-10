package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestSecretGet_HelpOutput(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"secret", "get", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	wantStrings := []string{"postgres", "kratos", "grafana", "openbao", "--json", "--env"}
	for _, want := range wantStrings {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q; got:\n%s", want, out)
		}
	}
}

func TestSecretGet_MutuallyExclusiveFlags(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"secret", "get", "postgres", "--json", "--env"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for --json and --env together, got nil")
	}
}

func TestSecretGet_RequiresArgument(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"secret", "get"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error when no argument provided, got nil")
	}
}

func TestSecretGet_TooManyArguments(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"secret", "get", "postgres", "kratos"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for too many arguments, got nil")
	}
}

func TestSecretGet_UnknownAlias_NoSource(t *testing.T) {
	// With a non-existent config and no .credentials, any alias should surface
	// a "no secret source available" message.
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	// Use a non-existent output dir so .credentials file cannot be read.
	root.SetArgs([]string{
		"secret", "get", "postgres",
		"--config", "/nonexistent/vibewarden.yaml",
		"--output-dir", "/nonexistent/dir",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error when no secret source available, got nil")
	}
}

func TestSecretCmd_HelpShowsGetAndList(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"secret", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"get", "list", "generate"} {
		if !strings.Contains(out, want) {
			t.Errorf("secret help missing subcommand %q; got:\n%s", want, out)
		}
	}
}
