package cmd_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestDoctorCmd_Registered verifies that the doctor subcommand is reachable
// from the root command.
func TestDoctorCmd_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")

	doctorCmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Find(doctor) error: %v", err)
	}
	if doctorCmd == nil || doctorCmd.Use != "doctor" {
		t.Fatal("expected 'doctor' subcommand to be registered on root")
	}
}

// TestDoctorCmd_FlagsRegistered verifies all expected flags are present.
func TestDoctorCmd_FlagsRegistered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	flags := []string{"config", "json", "skip-le-preflight"}
	for _, f := range flags {
		if doctorCmd.Flags().Lookup(f) == nil {
			t.Errorf("expected --%s flag to be registered on 'doctor' command", f)
		}
	}
}

// TestDoctorCmd_SkipLEPreflightFlag_HelpText verifies the skip flag is
// described in the help text (users discover flags via --help).
func TestDoctorCmd_SkipLEPreflightFlag_HelpText(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"doctor", "--help"})

	// Help command does not return an error.
	_ = root.Execute()

	help := outBuf.String()
	if !strings.Contains(help, "skip-le-preflight") {
		t.Errorf("expected --skip-le-preflight in help text\ngot:\n%s", help)
	}
}

// TestDoctorCmd_LongDescription_MentionsPreflight verifies the Long description
// references the LE preflight check so operators can discover it.
func TestDoctorCmd_LongDescription_MentionsPreflight(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	if !strings.Contains(doctorCmd.Long, "rate-limit") {
		t.Error("expected 'rate-limit' mentioned in doctor Long description")
	}
}
