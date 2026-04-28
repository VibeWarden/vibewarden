package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// runAddTLSCmd runs `vibew add tls <extraArgs...> <dir>` and returns separate
// stdout and stderr buffers so tests can assert on hint messages independently.
func runAddTLSCmd(t *testing.T, dir string, extraArgs ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	allArgs := append([]string{"add", "tls"}, extraArgs...)
	allArgs = append(allArgs, dir)
	root.SetArgs(allArgs)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// tlsMinimalBase is a minimal vibewarden.yaml with TLS disabled, used as the
// starting point for all add-tls integration tests.
const tlsMinimalBase = `# vibewarden.yaml
server:
  host: "127.0.0.1"
  port: 8080
upstream:
  host: "127.0.0.1"
  port: 3000
log:
  level: "info"
  format: "json"
security_headers:
  enabled: true
tls:
  enabled: false
`

// TestAddTLSCmd_AutoProvider covers the full provider auto-derivation matrix
// defined in issue #1188.
func TestAddTLSCmd_AutoProvider(t *testing.T) {
	tests := []struct {
		name string
		// base is the content of vibewarden.yaml written before the command.
		base string
		// args are the extra flags/values passed after "add tls".
		args []string
		// wantErr is true when the command should return a non-nil error.
		wantErr bool
		// wantInProd are substrings that MUST appear in vibewarden.production.yaml.
		wantInProd []string
		// notInProd are substrings that must NOT appear in vibewarden.production.yaml.
		notInProd []string
		// wantStderrContains is a substring that must appear in stderr output.
		wantStderrContains string
		// wantNoStderrHint asserts that no hint was printed to stderr.
		wantNoStderrHint bool
	}{
		{
			name:             "LE-compatible domain without explicit provider → letsencrypt + domain",
			base:             tlsMinimalBase,
			args:             []string{"--domain", "example.com"},
			wantInProd:       []string{"provider: letsencrypt", "domain: example.com"},
			wantNoStderrHint: true,
		},
		{
			name:               "LE-incompatible domain (localhost) without explicit provider → only domain + hint",
			base:               tlsMinimalBase,
			args:               []string{"--domain", "localhost"},
			wantInProd:         []string{"domain: localhost"},
			notInProd:          []string{"provider: letsencrypt"},
			wantStderrContains: "--provider self-signed",
		},
		{
			name:               "LE-incompatible domain (IP literal) without explicit provider → only domain + hint",
			base:               tlsMinimalBase,
			args:               []string{"--domain", "192.168.1.1"},
			wantInProd:         []string{"domain: 192.168.1.1"},
			notInProd:          []string{"provider: letsencrypt"},
			wantStderrContains: "--provider",
		},
		{
			name:               "LE-incompatible domain (.local TLD) without explicit provider → only domain + hint",
			base:               tlsMinimalBase,
			args:               []string{"--domain", "myapp.local"},
			wantInProd:         []string{"domain: myapp.local"},
			notInProd:          []string{"provider: letsencrypt"},
			wantStderrContains: "--provider",
		},
		{
			name:             "LE-compatible domain + explicit --provider external → external + domain",
			base:             tlsMinimalBase,
			args:             []string{"--domain", "example.com", "--provider", "external"},
			wantInProd:       []string{"provider: external", "domain: example.com"},
			wantNoStderrHint: true,
		},
		{
			name:             "LE-compatible domain + explicit --provider letsencrypt → letsencrypt + domain",
			base:             tlsMinimalBase,
			args:             []string{"--domain", "example.com", "--provider", "letsencrypt"},
			wantInProd:       []string{"provider: letsencrypt", "domain: example.com"},
			wantNoStderrHint: true,
		},
		{
			name: "LE-incompatible domain + explicit --provider self-signed → self-signed + domain; no hint",
			base: tlsMinimalBase,
			args: []string{"--domain", "localhost", "--provider", "self-signed"},
			// When --provider is explicit, provider IS written even for LE-incompatible domains.
			wantInProd:       []string{"provider: self-signed", "domain: localhost"},
			wantNoStderrHint: true,
		},
		{
			name:    "unknown --provider returns error before any file write",
			base:    tlsMinimalBase,
			args:    []string{"--domain", "example.com", "--provider", "unknownca"},
			wantErr: true,
		},
		{
			name:    "missing --domain returns error",
			base:    tlsMinimalBase,
			args:    []string{},
			wantErr: true,
		},
		{
			// The preferred resolution from the architect note: the seed factory no
			// longer seeds provider: letsencrypt, so a fresh file for an
			// LE-incompatible domain without explicit provider must NOT contain
			// provider: letsencrypt.
			name:       "fresh-init + localhost domain → production.yaml does NOT contain provider: letsencrypt",
			base:       tlsMinimalBase,
			args:       []string{"--domain", "localhost"},
			notInProd:  []string{"provider: letsencrypt"},
			wantInProd: []string{"domain: localhost"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := scaffoldTestDir(t, false)

			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tt.base), 0o644); err != nil {
				t.Fatalf("setup vibewarden.yaml: %v", err)
			}

			stdout, stderr, err := runAddTLSCmd(t, dir, tt.args...)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v (stdout=%q stderr=%q)", err, tt.wantErr, stdout, stderr)
			}
			if tt.wantErr {
				return
			}

			// Read back the production file.
			prodPath := filepath.Join(dir, "vibewarden.production.yaml")
			prod, err := os.ReadFile(prodPath)
			if err != nil {
				t.Fatalf("reading vibewarden.production.yaml: %v", err)
			}
			prodStr := string(prod)

			for _, want := range tt.wantInProd {
				if !strings.Contains(prodStr, want) {
					t.Errorf("production.yaml missing %q\n\n%s", want, prodStr)
				}
			}
			for _, notWant := range tt.notInProd {
				if strings.Contains(prodStr, notWant) {
					t.Errorf("production.yaml must NOT contain %q\n\n%s", notWant, prodStr)
				}
			}

			if tt.wantStderrContains != "" && !strings.Contains(stderr, tt.wantStderrContains) {
				t.Errorf("stderr %q does not contain %q", stderr, tt.wantStderrContains)
			}
			if tt.wantNoStderrHint && strings.Contains(stderr, "--provider") {
				t.Errorf("unexpected hint in stderr: %q", stderr)
			}
		})
	}
}

// TestAddTLSCmd_TLSAlreadyEnabled_AutoProvider verifies that the auto-provider
// logic also fires on the "TLS already enabled" fast path (no vibewarden.yaml
// modification, only production.yaml is touched).
func TestAddTLSCmd_TLSAlreadyEnabled_AutoProvider(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantInProd []string
		notInProd  []string
		wantHint   bool
	}{
		{
			name:       "LE-compatible domain auto-sets letsencrypt in production",
			args:       []string{"--domain", "mysite.com"},
			wantInProd: []string{"provider: letsencrypt", "domain: mysite.com"},
		},
		{
			name:       "LE-incompatible domain (localhost) does not set letsencrypt in production",
			args:       []string{"--domain", "localhost"},
			wantInProd: []string{"domain: localhost"},
			notInProd:  []string{"provider: letsencrypt"},
			wantHint:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := scaffoldTestDir(t, false)

			// TLS already enabled in base config.
			base := "tls:\n  enabled: true\n  provider: self-signed\n"
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(base), 0o644); err != nil {
				t.Fatalf("setup vibewarden.yaml: %v", err)
			}

			_, stderr, err := runAddTLSCmd(t, dir, tt.args...)
			if err != nil {
				t.Fatalf("Execute() error: %v", err)
			}

			prod, err := os.ReadFile(filepath.Join(dir, "vibewarden.production.yaml"))
			if err != nil {
				t.Fatalf("reading production.yaml: %v", err)
			}
			prodStr := string(prod)

			for _, want := range tt.wantInProd {
				if !strings.Contains(prodStr, want) {
					t.Errorf("production.yaml missing %q\n\n%s", want, prodStr)
				}
			}
			for _, notWant := range tt.notInProd {
				if strings.Contains(prodStr, notWant) {
					t.Errorf("production.yaml must NOT contain %q\n\n%s", notWant, prodStr)
				}
			}

			if tt.wantHint && !strings.Contains(stderr, "--provider") {
				t.Errorf("expected hint in stderr, got: %q", stderr)
			}
		})
	}
}

// TestAddTLSCmd_NewRootCmd_StderrSeparate verifies that the hint is written to
// stderr independently from stdout when using the full root command (the same
// code path as real CLI usage). This catches regressions where hint output is
// accidentally merged onto stdout.
func TestAddTLSCmd_NewRootCmd_StderrSeparate(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tlsMinimalBase), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"add", "tls", "--domain", "localhost", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stdout := outBuf.String()
	stderr := errBuf.String()

	if strings.Contains(stdout, "--provider") {
		t.Errorf("hint must not appear on stdout, got: %q", stdout)
	}
	if !strings.Contains(stderr, "--provider") {
		t.Errorf("hint must appear on stderr, got stderr=%q stdout=%q", stderr, stdout)
	}
}
