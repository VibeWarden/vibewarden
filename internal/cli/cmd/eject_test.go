package cmd_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestEjectCmd_DefaultConfig(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: false
  provider: self-signed
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"eject", "--config", path})

	err := root.Execute()
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if out == "" {
		t.Fatal("expected non-empty output, got empty string")
	}

	// Output must be valid JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	// Caddy config must contain an "apps" key.
	if _, ok := result["apps"]; !ok {
		t.Errorf("expected 'apps' key in output, got keys: %v", mapKeys(result))
	}
}

func TestEjectCmd_TLSEnabled(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8443
upstream:
  port: 3000
tls:
  enabled: true
  provider: self-signed
  domain: localhost
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"eject", "--config", path})

	err := root.Execute()
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v\nstderr: %s", err, errBuf.String())
	}

	var result map[string]any
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	apps, _ := result["apps"].(map[string]any)
	if apps == nil {
		t.Fatal("expected 'apps' to be a map")
	}

	// With TLS enabled Caddy config includes a "tls" section.
	if _, ok := apps["tls"]; !ok {
		t.Errorf("expected 'tls' key in apps, got: %v", mapKeys(apps))
	}
}

func TestEjectCmd_FormatFlag(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{"caddy explicit", "caddy", false},
		{"nginx unsupported", "nginx", true},
		{"traefik unsupported", "traefik", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
`)

			root := cmd.NewRootCmd("test")
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs([]string{"eject", "--config", path, "--format", tt.format})

			err := root.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v\nstderr: %s", err, tt.wantErr, errBuf.String())
			}
		})
	}
}

func TestEjectCmd_FileNotFound(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"eject", "--config", "/nonexistent/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for nonexistent config file, got nil")
	}
}

func TestEjectCmd_OutputIsIndentedJSON(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
`)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"eject", "--config", path})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	// Indented JSON contains newlines and spaces inside the object.
	if !strings.Contains(out, "\n") {
		t.Error("expected indented JSON (with newlines), got a single line")
	}
	if !strings.Contains(out, "  ") {
		t.Error("expected indented JSON (with spaces), got unindented output")
	}
}

func TestEjectCmd_ShorthandConfigFlag(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"eject", "-c", path})

	err := root.Execute()
	if err != nil {
		t.Fatalf("Execute() unexpected error using -c shorthand: %v\nstderr: %s", err, errBuf.String())
	}

	var result map[string]any
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestEjectCmd_ContainsHTTPSection(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
`)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"eject", "--config", path})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	apps, _ := result["apps"].(map[string]any)
	if apps == nil {
		t.Fatal("expected 'apps' key to be a map")
	}
	if _, ok := apps["http"]; !ok {
		t.Errorf("expected 'http' key under apps, got: %v", mapKeys(apps))
	}
}

// mapKeys returns the string keys of a map, for use in error messages.
func mapKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
