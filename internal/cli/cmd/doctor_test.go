package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// execDoctor runs "vibew doctor [args...]" and returns stdout, stderr, and the
// cobra error.
func execDoctor(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "vibew"}
	root.AddCommand(cmd.NewDoctorCmd())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"doctor"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

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

	flags := []string{"config", "json", "skip-le-preflight", "preflight"}
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

// TestDoctorCmd_PreflightFlag_Registered verifies that --preflight is registered.
func TestDoctorCmd_PreflightFlag_Registered(t *testing.T) {
	root := cmd.NewRootCmd("test")
	doctorCmd, _, _ := root.Find([]string{"doctor"})

	f := doctorCmd.Flags().Lookup("preflight")
	if f == nil {
		t.Fatal("expected --preflight flag to be registered on 'doctor' command")
	}
	if f.DefValue != "" {
		t.Errorf("expected --preflight default to be empty string, got %q", f.DefValue)
	}
}

// TestDoctorCmd_PreflightFlag_HelpText verifies --preflight appears in help text.
func TestDoctorCmd_PreflightFlag_HelpText(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"doctor", "--help"})
	_ = root.Execute()

	help := outBuf.String()
	if !strings.Contains(help, "preflight") {
		t.Errorf("expected --preflight in help text\ngot:\n%s", help)
	}
}

// TestDoctorCmd_PreflightFlag_MissingEnvFile_ErrorsClearly verifies that
// --preflight <env> with a missing vibewarden.<env>.yaml produces a clear
// error message containing the env file name and exits non-zero. No doctor
// checks should run in this case.
func TestDoctorCmd_PreflightFlag_MissingEnvFile_ErrorsClearly(t *testing.T) {
	dir := t.TempDir()
	// Only the base config exists; override is missing.
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"),
		[]byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	_, _, err := execDoctor(t, []string{"--preflight", "production"})
	if err == nil {
		t.Error("expected non-nil error for missing env override file, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "vibewarden.production.yaml") {
		t.Errorf("expected error message to mention vibewarden.production.yaml, got: %q", errMsg)
	}
}

// TestDoctorCmd_PreflightFlag_MissingBaseConfig_ErrorsClearly verifies that
// when vibewarden.yaml itself is absent, the error reports the base config
// as missing — not the override file. This is the ErrBaseConfigMissing branch.
func TestDoctorCmd_PreflightFlag_MissingBaseConfig_ErrorsClearly(t *testing.T) {
	dir := t.TempDir()
	// Deliberately do NOT write vibewarden.yaml — neither base nor override exists.

	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	_, _, err := execDoctor(t, []string{"--preflight", "production"})
	if err == nil {
		t.Error("expected non-nil error when vibewarden.yaml is absent, got nil")
	}
	errMsg := err.Error()
	// Must name the base config, not the override file.
	if !strings.Contains(errMsg, "vibewarden.yaml") {
		t.Errorf("expected error message to mention vibewarden.yaml, got: %q", errMsg)
	}
	// Must NOT report the override file as missing when the base is what's gone.
	if strings.Contains(errMsg, "vibewarden.production.yaml") {
		t.Errorf("error message must not mention vibewarden.production.yaml when the base config is missing, got: %q", errMsg)
	}
	// Must contain the vibew init hint.
	if !strings.Contains(errMsg, "vibew init") {
		t.Errorf("expected 'vibew init' hint in error message, got: %q", errMsg)
	}
}

// TestDoctorCmd_PreflightFlag_AbsentByDefault verifies that when --preflight is
// not set, no "Preflight:" section appears in the output.
func TestDoctorCmd_PreflightFlag_AbsentByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"),
		[]byte("server:\n  port: 8443\ntls:\n  provider: external\n"), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	stdout, _, _ := execDoctor(t, nil)
	if strings.Contains(stdout, "Preflight:") {
		t.Errorf("expected no 'Preflight:' section when --preflight flag is absent\ngot:\n%s", stdout)
	}
}
