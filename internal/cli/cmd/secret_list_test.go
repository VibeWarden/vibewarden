package cmd_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestSecretList_HelpOutput(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"secret", "list", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	wantStrings := []string{"--json", "alias", "OpenBao"}
	for _, want := range wantStrings {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q; got:\n%s", want, out)
		}
	}
}

func TestSecretList_HumanOutput_ContainsAliases(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	// Use a non-existent dir: OpenBao will be unreachable, only aliases returned.
	root.SetArgs([]string{
		"secret", "list",
		"--config", "/nonexistent/vibewarden.yaml",
		"--output-dir", "/nonexistent/dir",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, alias := range []string{"postgres", "kratos", "grafana", "openbao"} {
		if !strings.Contains(out, alias) {
			t.Errorf("output missing alias %q; got:\n%s", alias, out)
		}
	}
}

func TestSecretList_JSONOutput(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{
		"secret", "list",
		"--json",
		"--config", "/nonexistent/vibewarden.yaml",
		"--output-dir", "/nonexistent/dir",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	var paths []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf.String())), &paths); err != nil {
		t.Fatalf("output is not valid JSON array: %v\noutput: %s", err, outBuf.String())
	}

	if len(paths) < 4 {
		t.Errorf("JSON output has %d paths, want at least 4", len(paths))
	}
}
