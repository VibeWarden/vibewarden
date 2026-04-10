package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestUpgradeCmd_ExistsAndRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	// Find the upgrade subcommand.
	var found bool
	for _, sub := range root.Commands() {
		if sub.Use == "upgrade [version]" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("upgrade command not registered in root")
	}
}

func TestUpgradeCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)

	root.SetArgs([]string{"upgrade", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error from --help: %v", err)
	}

	help := out.String()
	for _, want := range []string{
		"upgrade",
		"dry-run",
		"install-dir",
		"version",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("expected %q in help output, got:\n%s", want, help)
		}
	}
}

func TestUpgradeCmd_TooManyArgs(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"upgrade", "v1.0.0", "v2.0.0"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for too many args, got nil")
	}
}

func TestUpgradeCmd_DryRunFlagDefault(t *testing.T) {
	upgradeCmd := cmd.NewUpgradeCmd()
	f := upgradeCmd.Flags().Lookup("dry-run")
	if f == nil {
		t.Fatal("--dry-run flag not found")
	}
	if f.DefValue != "false" {
		t.Errorf("--dry-run default = %q, want %q", f.DefValue, "false")
	}
}

func TestUpgradeCmd_InstallDirFlagDefault(t *testing.T) {
	upgradeCmd := cmd.NewUpgradeCmd()
	f := upgradeCmd.Flags().Lookup("install-dir")
	if f == nil {
		t.Fatal("--install-dir flag not found")
	}
	if f.DefValue != "" {
		t.Errorf("--install-dir default = %q, want empty string", f.DefValue)
	}
}
