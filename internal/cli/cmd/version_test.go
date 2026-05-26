package cmd

import (
	"bytes"
	"testing"
)

// TestVersionCmd_SubcommandMatchesFlagOutput asserts that "vibew version" and
// "vibew --version" produce identical stdout. This is the behavioural contract:
// both invocations must agree bit-for-bit so tooling that parses the version
// line works regardless of which form was used.
func TestVersionCmd_SubcommandMatchesFlagOutput(t *testing.T) {
	const testVersion = "v1.2.3-test"

	// Capture output of "vibew --version".
	var flagOut bytes.Buffer
	flagRoot := NewRootCmd(testVersion)
	flagRoot.SetOut(&flagOut)
	flagRoot.SetErr(bytes.NewBuffer(nil))
	flagRoot.SetArgs([]string{"--version"})
	if err := flagRoot.Execute(); err != nil {
		t.Fatalf("--version returned error: %v", err)
	}
	flagOutput := flagOut.String()

	// Capture output of "vibew version".
	var subcmdOut bytes.Buffer
	subcmdRoot := NewRootCmd(testVersion)
	subcmdRoot.SetOut(&subcmdOut)
	subcmdRoot.SetErr(bytes.NewBuffer(nil))
	subcmdRoot.SetArgs([]string{"version"})
	if err := subcmdRoot.Execute(); err != nil {
		t.Fatalf("version subcommand returned error: %v", err)
	}
	subcmdOutput := subcmdOut.String()

	if flagOutput != subcmdOutput {
		t.Errorf("output mismatch:\n  --version  = %q\n  version    = %q", flagOutput, subcmdOutput)
	}
}

// TestVersionCmd_OutputFormat verifies the exact output format for both
// invocations: "vibew <version>\n".
func TestVersionCmd_OutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		version string
		args    []string
	}{
		{"subcommand with semver", "v0.19.0", []string{"version"}},
		{"subcommand with dev version", "dev", []string{"version"}},
		{"subcommand with prerelease", "v1.0.0-rc.1", []string{"version"}},
		{"flag with semver", "v0.19.0", []string{"--version"}},
		{"flag with dev version", "dev", []string{"--version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			root := NewRootCmd(tt.version)
			root.SetOut(&out)
			root.SetErr(bytes.NewBuffer(nil))
			root.SetArgs(tt.args)

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute(%v) returned error: %v", tt.args, err)
			}

			got := out.String()
			want := "vibew " + tt.version + "\n"
			if got != want {
				t.Errorf("Execute(%v) output = %q, want %q", tt.args, got, want)
			}
		})
	}
}

// TestVersionCmd_IsRegistered asserts that "vibew version" is a known
// subcommand — running it must not return an "unknown command" error.
func TestVersionCmd_IsRegistered(t *testing.T) {
	root := NewRootCmd("test")
	root.SetOut(bytes.NewBuffer(nil))
	root.SetErr(bytes.NewBuffer(nil))
	root.SetArgs([]string{"version"})

	err := root.Execute()
	if err != nil {
		t.Errorf("Execute([version]) returned unexpected error: %v", err)
	}
}
