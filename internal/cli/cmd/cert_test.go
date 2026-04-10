package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// fakePEM is a syntactically valid-looking PEM block used in tests.
const fakePEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0fake0fake0fake0fake==
-----END CERTIFICATE-----
`

func TestCertExport_OutputsPEM(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "root.crt")
	if err := os.WriteFile(certPath, []byte(fakePEM), 0o644); err != nil {
		t.Fatalf("writing temp cert: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"cert", "export", "--cert-path", certPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	got := outBuf.String()
	if got != fakePEM {
		t.Errorf("output = %q, want %q", got, fakePEM)
	}
}

func TestCertExport_PathFlag(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "root.crt")
	if err := os.WriteFile(certPath, []byte(fakePEM), 0o644); err != nil {
		t.Fatalf("writing temp cert: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"cert", "export", "--cert-path", certPath, "--path"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	got := strings.TrimSpace(outBuf.String())
	if got != certPath {
		t.Errorf("output = %q, want %q", got, certPath)
	}
}

func TestCertExport_NotFound(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	// Point to a directory that does not exist so all candidates miss.
	root.SetArgs([]string{"cert", "export", "--cert-path", "/nonexistent/path/root.crt"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for missing cert, got nil")
	}
}

func TestCertExport_PathFlag_NotFound(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"cert", "export", "--cert-path", "/nonexistent/path/root.crt", "--path"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for missing cert with --path flag, got nil")
	}
}

func TestCertCmd_HelpWhenNoSubcommand(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"cert", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "export") {
		t.Errorf("help output does not mention 'export', got: %q", out)
	}
}
