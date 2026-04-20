package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestTLSCmd_HelpWhenNoSubcommand(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"tls", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "status") {
		t.Errorf("help output does not mention 'status', got: %q", out)
	}
}

func TestTLSStatusCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, err := root.Find([]string{"tls", "status"})
	if err != nil {
		t.Fatalf("Find(tls status) error: %v", err)
	}
	if statusCmd == nil || statusCmd.Use != "status" {
		t.Fatal("expected 'status' subcommand to be registered on 'tls'")
	}
}

func TestTLSStatusCmd_RequiresDomain(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"tls", "status", "--target", "ssh://user@host"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for missing --domain, got nil")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("error = %q, want it to mention 'domain'", err.Error())
	}
}

func TestTLSStatusCmd_RequiresTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"tls", "status", "--domain", "example.com"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for missing --target, got nil")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error = %q, want it to mention 'target'", err.Error())
	}
}

func TestTLSStatusCmd_InvalidTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"tls", "status", "--domain", "example.com", "--target", "http://invalid"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for invalid target, got nil")
	}
	if !strings.Contains(err.Error(), "ssh") {
		t.Errorf("error = %q, want it to mention 'ssh'", err.Error())
	}
}

func TestTLSStatusCmd_HasFlags(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, _ := root.Find([]string{"tls", "status"})

	flags := []string{"domain", "target", "ssh-key", "port"}
	for _, flag := range flags {
		if statusCmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s flag on 'tls status' command", flag)
		}
	}
}

func TestTLSStatusCmd_DefaultPort(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, _ := root.Find([]string{"tls", "status"})

	portFlag := statusCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("expected --port flag")
	}
	if portFlag.DefValue != "443" {
		t.Errorf("--port default = %q, want %q", portFlag.DefValue, "443")
	}
}

func TestTLSStatusCmd_HelpContainsExpectedContent(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"tls", "status", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"domain", "target", "openssl", "certificate", "SSH"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q, got: %q", want, out)
		}
	}
}
