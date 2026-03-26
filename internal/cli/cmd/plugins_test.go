package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestPluginsCmd_ListsAllPlugins(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"plugins"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()

	// All compiled-in plugin names must appear in the output.
	wantPlugins := []string{
		"tls",
		"security-headers",
		"rate-limiting",
		"auth",
		"metrics",
		"user-management",
	}
	for _, name := range wantPlugins {
		if !strings.Contains(out, name) {
			t.Errorf("plugin %q not found in plugins output:\n%s", name, out)
		}
	}

	// Header columns must appear.
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected NAME header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ENABLED") {
		t.Errorf("expected ENABLED header in output, got:\n%s", out)
	}
}

func TestPluginsCmd_ShowsEnabledStatus(t *testing.T) {
	// With default config (no config file), rate-limiting and metrics default
	// to enabled=true; the rest default to false.
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&outBuf)
	root.SetArgs([]string{"plugins"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	// At least one "true" and one "false" must appear.
	if !strings.Contains(out, "true") {
		t.Errorf("expected at least one enabled=true plugin in output:\n%s", out)
	}
	if !strings.Contains(out, "false") {
		t.Errorf("expected at least one enabled=false plugin in output:\n%s", out)
	}
}

func TestPluginsCmd_WithConfigFile(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: true
  provider: self-signed
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"plugins", "--config", path})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "tls") {
		t.Errorf("expected tls in output, got:\n%s", out)
	}
}

func TestPluginsShowCmd_KnownPlugin(t *testing.T) {
	tests := []struct {
		pluginName  string
		wantStrings []string
	}{
		{
			pluginName:  "tls",
			wantStrings: []string{"Plugin: tls", "provider", "enabled"},
		},
		{
			pluginName:  "security-headers",
			wantStrings: []string{"Plugin: security-headers", "frame_option"},
		},
		{
			pluginName:  "rate-limiting",
			wantStrings: []string{"Plugin: rate-limiting", "per_ip"},
		},
		{
			pluginName:  "auth",
			wantStrings: []string{"Plugin: auth", "kratos_public_url"},
		},
		{
			pluginName:  "metrics",
			wantStrings: []string{"Plugin: metrics", "path_patterns"},
		},
		{
			pluginName:  "user-management",
			wantStrings: []string{"Plugin: user-management", "admin_token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.pluginName, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs([]string{"plugins", "show", tt.pluginName})

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
			}

			out := outBuf.String()
			for _, want := range tt.wantStrings {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output for plugin %q:\n%s", want, tt.pluginName, out)
				}
			}

			// Every show output must include an Example section.
			if !strings.Contains(out, "Example:") {
				t.Errorf("expected Example: section in output for plugin %q:\n%s", tt.pluginName, out)
			}
		})
	}
}

func TestPluginsShowCmd_UnknownPlugin(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"plugins", "show", "nonexistent-plugin"})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for unknown plugin, got nil")
	}
}

func TestPluginsShowCmd_RequiresExactlyOneArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{"plugins", "show"}},
		{"two args", []string{"plugins", "show", "tls", "metrics"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs(tt.args)

			if err := root.Execute(); err == nil {
				t.Errorf("Execute() expected error for %q, got nil", tt.name)
			}
		})
	}
}
