package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// writeConfig is a test helper that writes content to a temp vibewarden.yaml
// and returns the file path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestValidateCmd_ValidConfig(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: false
  provider: self-signed
log:
  level: info
  format: json
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err != nil {
		t.Errorf("Execute() expected no error for valid config, got: %v\nstderr: %s", err, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "valid") {
		t.Errorf("expected success message, got: %q", out)
	}
}

func TestValidateCmd_DefaultPath(t *testing.T) {
	// No positional argument — the command tries ./vibewarden.yaml which does
	// not exist in the test's working dir. config.Load() with no path should
	// not return an error (viper treats file-not-found as non-fatal), so we
	// expect a success message with default values.
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate"})

	// We don't assert on the outcome here because the CWD may or may not have
	// a real vibewarden.yaml. We just verify the command doesn't panic.
	_ = root.Execute()
}

func TestValidateCmd_FileNotFound(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", "/nonexistent/path/vibewarden.yaml"})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for nonexistent file, got nil")
	}
}

func TestValidateCmd_InvalidPortValues(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErrFrag string
	}{
		{
			name: "server port zero",
			yaml: `
server:
  port: 0
upstream:
  port: 3000
`,
			wantErrFrag: "server.port",
		},
		{
			name: "upstream port out of range",
			yaml: `
server:
  port: 8080
upstream:
  port: 99999
`,
			wantErrFrag: "upstream.port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.yaml)

			root := cmd.NewRootCmd("test")
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs([]string{"validate", path})

			err := root.Execute()
			if err == nil {
				t.Errorf("Execute() expected error, got nil")
			}

			errOut := errBuf.String()
			if !strings.Contains(errOut, tt.wantErrFrag) {
				t.Errorf("stderr %q does not contain %q", errOut, tt.wantErrFrag)
			}
		})
	}
}

func TestValidateCmd_InvalidTLSProvider(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: false
  provider: cloudflare
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for invalid tls.provider, got nil")
	}

	if !strings.Contains(errBuf.String(), "tls.provider") {
		t.Errorf("expected tls.provider mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_LetsEncryptRequiresDomain(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: true
  provider: letsencrypt
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error when letsencrypt has no domain, got nil")
	}

	if !strings.Contains(errBuf.String(), "tls.domain") {
		t.Errorf("expected tls.domain mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_ExternalTLSRequiresCertAndKey(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
tls:
  enabled: true
  provider: external
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error when external TLS has no cert/key, got nil")
	}

	errOut := errBuf.String()
	if !strings.Contains(errOut, "tls.cert_path") {
		t.Errorf("expected tls.cert_path mention in stderr, got: %q", errOut)
	}
	if !strings.Contains(errOut, "tls.key_path") {
		t.Errorf("expected tls.key_path mention in stderr, got: %q", errOut)
	}
}

func TestValidateCmd_AdminTokenRequiredWhenEnabled(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
admin:
  enabled: true
  token: ""
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error when admin.enabled but no token, got nil")
	}

	if !strings.Contains(errBuf.String(), "admin.token") {
		t.Errorf("expected admin.token mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_InvalidLogLevel(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
log:
  level: verbose
  format: json
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for invalid log.level, got nil")
	}

	if !strings.Contains(errBuf.String(), "log.level") {
		t.Errorf("expected log.level mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_InvalidLogFormat(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
log:
  level: info
  format: xml
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for invalid log.format, got nil")
	}

	if !strings.Contains(errBuf.String(), "log.format") {
		t.Errorf("expected log.format mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_InvalidFrameOption(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8080
upstream:
  port: 3000
security_headers:
  enabled: true
  frame_option: ALLOWALL
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for invalid frame_option, got nil")
	}

	if !strings.Contains(errBuf.String(), "frame_option") {
		t.Errorf("expected frame_option mention in stderr, got: %q", errBuf.String())
	}
}

func TestValidateCmd_MultipleErrors(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 0
upstream:
  port: 99999
tls:
  enabled: false
  provider: bad-provider
log:
  level: verbose
  format: xml
`)

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"validate", path})

	err := root.Execute()
	if err == nil {
		t.Error("Execute() expected error for multiple invalid fields, got nil")
	}

	errOut := errBuf.String()
	for _, want := range []string{"server.port", "upstream.port", "tls.provider", "log.level", "log.format"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("expected %q in stderr, got: %q", want, errOut)
		}
	}
}

func TestValidateConfig_ValidDefaults(t *testing.T) {
	// Test the exported validateConfig logic (accessed via CLI) with a
	// minimal valid YAML that relies on defaults.
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
	root.SetArgs([]string{"validate", path})

	if err := root.Execute(); err != nil {
		t.Errorf("Execute() unexpected error for minimal valid config: %v\nstderr: %s", err, errBuf.String())
	}
}
