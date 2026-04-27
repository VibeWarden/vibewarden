package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestObsCmd_HelpContainsSubcommands(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"obs", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("obs --help returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{"up", "down", "observability"} {
		if !strings.Contains(output, want) {
			t.Errorf("obs --help missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestObsCmd_UpHelp(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"obs", "up", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("obs up --help returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{"Prometheus", "Grafana", "--config", "--verbose"} {
		if !strings.Contains(output, want) {
			t.Errorf("obs up --help missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestObsCmd_DownHelp(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"obs", "down", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("obs down --help returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{"--volumes", "--remove-orphans", "--yes"} {
		if !strings.Contains(output, want) {
			t.Errorf("obs down --help missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestObsCmd_IsRegisteredOnRoot(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}

	if !strings.Contains(out.String(), "obs") {
		t.Errorf("root help does not mention 'obs' command\ngot:\n%s", out.String())
	}
}
