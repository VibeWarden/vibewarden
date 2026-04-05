package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestDeployCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	deployCmd, _, err := root.Find([]string{"deploy"})
	if err != nil {
		t.Fatalf("Find(deploy) error: %v", err)
	}
	if deployCmd == nil {
		t.Fatal("expected 'deploy' command to be registered")
	}
}

func TestDeployCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	deployCmd, _, err := root.Find([]string{"deploy"})
	if err != nil {
		t.Fatalf("Find(deploy) error: %v", err)
	}
	if deployCmd.Flags().Lookup("target") == nil {
		t.Error("expected --target flag on deploy command")
	}
	if deployCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag on deploy command")
	}
}

func TestDeployCmd_MissingTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target is not provided")
	}
}

func TestDeployStatusCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, err := root.Find([]string{"deploy", "status"})
	if err != nil {
		t.Fatalf("Find(deploy status) error: %v", err)
	}
	if statusCmd == nil {
		t.Fatal("expected 'deploy status' subcommand to be registered")
	}
}

func TestDeployStatusCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, err := root.Find([]string{"deploy", "status"})
	if err != nil {
		t.Fatalf("Find(deploy status) error: %v", err)
	}
	if statusCmd.Flags().Lookup("target") == nil {
		t.Error("expected --target flag on deploy status command")
	}
}

func TestDeployStatusCmd_MissingTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "status"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target is not provided for deploy status")
	}
}

func TestDeployLogsCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, err := root.Find([]string{"deploy", "logs"})
	if err != nil {
		t.Fatalf("Find(deploy logs) error: %v", err)
	}
	if logsCmd == nil {
		t.Fatal("expected 'deploy logs' subcommand to be registered")
	}
}

func TestDeployLogsCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	logsCmd, _, err := root.Find([]string{"deploy", "logs"})
	if err != nil {
		t.Fatalf("Find(deploy logs) error: %v", err)
	}
	if logsCmd.Flags().Lookup("target") == nil {
		t.Error("expected --target flag on deploy logs command")
	}
	if logsCmd.Flags().Lookup("lines") == nil {
		t.Error("expected --lines flag on deploy logs command")
	}
}

func TestDeployLogsCmd_MissingTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "logs"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target is not provided for deploy logs")
	}
}

func TestDeployCmd_InvalidTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "--target", "http://user@host"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target has wrong scheme")
	}
	if !strings.Contains(err.Error(), "invalid --target") {
		t.Errorf("expected 'invalid --target' in error, got: %v", err)
	}
}

func TestDeployStatusCmd_InvalidTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "status", "--target", "ftp://user@host"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target has wrong scheme")
	}
	if !strings.Contains(err.Error(), "invalid --target") {
		t.Errorf("expected 'invalid --target' in error, got: %v", err)
	}
}

func TestDeployLogsCmd_InvalidTarget(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "logs", "--target", "ftp://user@host"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --target has wrong scheme")
	}
	if !strings.Contains(err.Error(), "invalid --target") {
		t.Errorf("expected 'invalid --target' in error, got: %v", err)
	}
}

func TestDeployCmd_HelpFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"deploy", "--help"})

	// --help should not fail.
	err := root.Execute()
	if err != nil {
		t.Fatalf("expected no error for --help, got: %v", err)
	}
}

func TestDeployCmd_SubcommandHelp(t *testing.T) {
	subcmds := []string{"status", "logs"}
	for _, sub := range subcmds {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			root.SetArgs([]string{"deploy", sub, "--help"})

			err := root.Execute()
			if err != nil {
				t.Fatalf("expected no error for deploy %s --help, got: %v", sub, err)
			}
		})
	}
}
