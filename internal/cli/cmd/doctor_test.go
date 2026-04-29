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

// TestDoctorCmd_LongDescription_NoUpstreamReachable is a regression guard that
// ensures the misleading upstream-reachable check is not re-added to the help
// text. The upstream lives on the docker-compose internal network and was never
// reachable from the host; the check only produced confusing WARN output.
func TestDoctorCmd_LongDescription_NoUpstreamReachable(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	forbidden := []string{
		"Upstream application is reachable",
		"Upstream reachable",
	}
	for _, s := range forbidden {
		if strings.Contains(doctorCmd.Long, s) {
			t.Errorf("doctor Long description must not mention %q (misleading upstream check was removed in #1198)", s)
		}
	}
}

// TestDoctorCmd_LongDescription_MentionsHealthEndpoint verifies the Long
// description points operators at _vibewarden/health for runtime upstream checks.
func TestDoctorCmd_LongDescription_MentionsHealthEndpoint(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	if !strings.Contains(doctorCmd.Long, "_vibewarden/health") {
		t.Error("expected '_vibewarden/health' mentioned in doctor Long description as runtime health pointer")
	}
}

// TestDoctorCmd_LongDescription_NoContainerHealth is a regression guard for
// #1222 — the Container health check was deleted entirely (covered by
// _vibewarden/health since #1197). The Long help text must not re-introduce the
// check as an advertised feature. The existing "does NOT probe runtime container
// health" disclaimer is intentional and is excluded from this guard.
// Mirrors TestDoctorCmd_LongDescription_NoUpstreamReachable (#1198).
func TestDoctorCmd_LongDescription_NoContainerHealth(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	// These are the phrases that would appear if the check were re-added as a
	// listed feature. The existing "does NOT probe runtime container health"
	// disclaimer is legitimate and does not contain these substrings.
	forbidden := []string{
		"- Container health",
		"Container health check",
		"container health is",
	}
	long := doctorCmd.Long
	for _, s := range forbidden {
		if strings.Contains(strings.ToLower(long), strings.ToLower(s)) {
			t.Errorf("doctor Long must not advertise 'container health' as a check — it was deleted in #1222\nmatched: %q\nLong:\n%s", s, long)
		}
	}
}
