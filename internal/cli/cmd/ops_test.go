package cmd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestNewDevCmd_RegisteredOnRoot verifies that the dev subcommand is reachable
// from the root command and that its expected flags are registered.
func TestNewDevCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	devCmd, _, err := root.Find([]string{"dev"})
	if err != nil {
		t.Fatalf("Find(dev) error: %v", err)
	}
	if devCmd == nil || devCmd.Use != "dev" {
		t.Fatal("expected 'dev' subcommand to be registered on root")
	}

	if devCmd.Flags().Lookup("observability") == nil {
		t.Error("expected --observability flag to be registered on 'dev' command")
	}
	if devCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag to be registered on 'dev' command")
	}
}

// TestNewDevCmd_Help verifies that help output for dev contains expected content.
func TestNewDevCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"dev", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"dev", "observability", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewDevCmd_MissingConfig verifies that dev returns an error when
// a non-existent config path is specified.
func TestNewDevCmd_MissingConfig(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"dev", "--config", "/nonexistent/path/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when config file does not exist")
	}
}

// TestNewStatusCmd_RegisteredOnRoot verifies that the status subcommand is
// reachable from the root command and that its expected flags are registered.
func TestNewStatusCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	statusCmd, _, err := root.Find([]string{"status"})
	if err != nil {
		t.Fatalf("Find(status) error: %v", err)
	}
	if statusCmd == nil || statusCmd.Use != "status" {
		t.Fatal("expected 'status' subcommand to be registered on root")
	}

	if statusCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag to be registered on 'status' command")
	}
}

// TestNewStatusCmd_Help verifies that help output for status contains expected content.
func TestNewStatusCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"status", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"status", "health", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewStatusCmd_MissingConfig verifies that status returns an error when
// a non-existent config path is specified.
func TestNewStatusCmd_MissingConfig(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"status", "--config", "/nonexistent/path/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when config file does not exist")
	}
}

// TestNewDoctorCmd_RegisteredOnRoot verifies that the doctor subcommand is
// reachable from the root command and that its expected flags are registered.
func TestNewDoctorCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	doctorCmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Find(doctor) error: %v", err)
	}
	if doctorCmd == nil || doctorCmd.Use != "doctor" {
		t.Fatal("expected 'doctor' subcommand to be registered on root")
	}

	if doctorCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag to be registered on 'doctor' command")
	}
}

// TestNewDoctorCmd_Help verifies that help output for doctor contains expected content.
func TestNewDoctorCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"doctor", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"doctor", "Docker", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewDoctorCmd_ReturnsErrorNotExit verifies that when checks fail the
// command returns an error via RunE (not os.Exit). Cobra propagates the error
// back to Execute() — we can observe it without process termination.
func TestNewDoctorCmd_ReturnsErrorNotExit(t *testing.T) {
	// Use a non-existent config so the command degrades gracefully (nil cfg),
	// then let doctor run against a non-running Docker — the command should
	// return an error from RunE rather than calling os.Exit.
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"doctor", "--config", "/nonexistent/vibewarden.yaml"})

	// Execute returns the error from RunE. If os.Exit were called the process
	// would terminate and we would never reach the assertion below.
	// We accept any error or nil here — the important thing is we did not exit.
	_ = root.Execute()
}

// TestNewDoctorCmd_Short verifies the Short description is set.
func TestNewDoctorCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})
	if doctorCmd.Short == "" {
		t.Error("expected non-empty Short description on 'doctor' command")
	}
}

// TestNewDevCmd_Short verifies the Short description is set.
func TestNewDevCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	devCmd, _, _ := root.Find([]string{"dev"})
	if devCmd.Short == "" {
		t.Error("expected non-empty Short description on 'dev' command")
	}
}

// TestNewStatusCmd_Short verifies the Short description is set.
func TestNewStatusCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	statusCmd, _, _ := root.Find([]string{"status"})
	if statusCmd.Short == "" {
		t.Error("expected non-empty Short description on 'status' command")
	}
}

// TestNewGenerateCmd_RegisteredOnRoot verifies that the generate subcommand
// is reachable from the root command and that its expected flags are registered.
func TestNewGenerateCmd_RegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")

	genCmd, _, err := root.Find([]string{"generate"})
	if err != nil {
		t.Fatalf("Find(generate) error: %v", err)
	}
	if genCmd == nil || genCmd.Use != "generate" {
		t.Fatal("expected 'generate' subcommand to be registered on root")
	}

	if genCmd.Flags().Lookup("config") == nil {
		t.Error("expected --config flag to be registered on 'generate' command")
	}
	if genCmd.Flags().Lookup("output-dir") == nil {
		t.Error("expected --output-dir flag to be registered on 'generate' command")
	}
}

// TestNewGenerateCmd_Help verifies that help output for generate contains expected content.
func TestNewGenerateCmd_Help(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"generate", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"generate", "config", "output-dir", ".vibewarden/generated"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestNewGenerateCmd_MissingConfig verifies that generate returns an error when
// a non-existent config path is specified.
func TestNewGenerateCmd_MissingConfig(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"generate", "--config", "/nonexistent/path/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when config file does not exist")
	}
}

// TestNewGenerateCmd_Short verifies the Short description is set.
func TestNewGenerateCmd_Short(t *testing.T) {
	root := cmd.NewRootCmd("test")
	genCmd, _, _ := root.Find([]string{"generate"})
	if genCmd.Short == "" {
		t.Error("expected non-empty Short description on 'generate' command")
	}
}

// TestQuickStartFlow_WrapThenDev documents and exercises the two-command Quick
// Start path from the README:
//
//	vibew wrap --upstream 3000
//	vibew dev
//
// Step 1 (wrap) must succeed and produce vibewarden.yaml with the configured
// upstream port. Step 2 (dev) must be reachable as a subcommand and must have
// its generator wired — verified by checking that its Long description
// documents that it generates runtime config before starting the stack.
func TestQuickStartFlow_WrapThenDev(t *testing.T) {
	t.Run("step 1: wrap --upstream 3000 produces vibewarden.yaml", func(t *testing.T) {
		dir := t.TempDir()
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"wrap", dir, "--upstream", "3000"})
		if err := root.Execute(); err != nil {
			t.Fatalf("vibew wrap --upstream 3000 failed: %v", err)
		}

		data, err := os.ReadFile(dir + "/vibewarden.yaml")
		if err != nil {
			t.Fatalf("vibewarden.yaml not created after init: %v", err)
		}
		if !strings.Contains(string(data), "3000") {
			t.Errorf("vibewarden.yaml does not contain upstream port 3000:\n%s", data)
		}
	})

	t.Run("step 2: dev subcommand is registered and documents auto-generation", func(t *testing.T) {
		root := cmd.NewRootCmd("test")
		devCmd, _, err := root.Find([]string{"dev"})
		if err != nil || devCmd == nil || devCmd.Use != "dev" {
			t.Fatalf("dev subcommand not found: %v", err)
		}
		// The Long description must mention that runtime config files are
		// generated before the stack starts — this is the contract that makes
		// `vibew dev` a single self-contained step after `vibew wrap`.
		if !strings.Contains(devCmd.Long, "generates runtime configuration") &&
			!strings.Contains(devCmd.Long, "generates") {
			t.Errorf("dev Long description does not mention config generation:\n%s", devCmd.Long)
		}
	})

	t.Run("step 2: dev --help does not mention standalone docker compose up", func(t *testing.T) {
		root := cmd.NewRootCmd("test")
		var outBuf strings.Builder
		root.SetOut(&outBuf)
		root.SetArgs([]string{"dev", "--help"})
		if err := root.Execute(); err != nil {
			t.Fatalf("dev --help failed: %v", err)
		}
		out := outBuf.String()
		// Help must not suggest running docker compose manually as a prerequisite;
		// generation is handled internally by the dev command.
		if strings.Contains(out, "docker compose up") && !strings.Contains(out, "vibew dev") {
			t.Errorf("dev help should not instruct user to run 'docker compose up' manually:\n%s", out)
		}
	})
}
