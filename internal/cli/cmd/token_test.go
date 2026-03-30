package cmd_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// generateTestKeyPair creates a temporary directory with a 2048-bit RSA private
// key written as a PEM-encoded PKCS#8 file. It returns the directory path and
// a DevKeyPair suitable for use in tests.
func generateTestKeyPair(t *testing.T) (dir string, kp *jwtadapter.DevKeyPair) {
	t.Helper()

	dir = t.TempDir()
	var err error
	kp, err = jwtadapter.LoadOrGenerateDevKeys(dir)
	if err != nil {
		t.Fatalf("generateTestKeyPair: %v", err)
	}
	return dir, kp
}

// ─── signDevToken (unit tests) ────────────────────────────────────────────────

func TestSignDevToken_ValidToken(t *testing.T) {
	_, kp := generateTestKeyPair(t)

	raw, err := cmd.SignDevToken(context.Background(), kp, "sub-1", "user@test.com", "Test User", "admin", time.Hour)
	if err != nil {
		t.Fatalf("SignDevToken() unexpected error: %v", err)
	}
	if raw == "" {
		t.Fatal("SignDevToken() returned empty token")
	}

	// Parse and validate the token against the public key.
	tok, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() unexpected error: %v", err)
	}

	var stdClaims josejwt.Claims
	var custom map[string]any
	if err := tok.Claims(&kp.PrivateKey.PublicKey, &stdClaims, &custom); err != nil {
		t.Fatalf("Claims() unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"issuer", stdClaims.Issuer, jwtadapter.DevIssuer},
		{"subject", stdClaims.Subject, "sub-1"},
		{"email", custom["email"], "user@test.com"},
		{"name", custom["name"], "Test User"},
		{"role", custom["role"], "admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("claim %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}

	// Audience slice must contain DevAudience.
	found := false
	for _, a := range stdClaims.Audience {
		if a == jwtadapter.DevAudience {
			found = true
		}
	}
	if !found {
		t.Errorf("audience %v does not contain %q", stdClaims.Audience, jwtadapter.DevAudience)
	}
}

func TestSignDevToken_KIDHeader(t *testing.T) {
	_, kp := generateTestKeyPair(t)

	raw, err := cmd.SignDevToken(context.Background(), kp, "u", "e@e.com", "N", "user", time.Minute)
	if err != nil {
		t.Fatalf("SignDevToken() unexpected error: %v", err)
	}

	tok, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() unexpected error: %v", err)
	}

	if len(tok.Headers) == 0 {
		t.Fatal("no JWT headers")
	}
	if got := tok.Headers[0].KeyID; got != jwtadapter.DevKID {
		t.Errorf("kid = %q, want %q", got, jwtadapter.DevKID)
	}
}

func TestSignDevToken_Expiry(t *testing.T) {
	_, kp := generateTestKeyPair(t)

	ttl := 2 * time.Hour
	before := time.Now()
	raw, err := cmd.SignDevToken(context.Background(), kp, "u", "e@e.com", "N", "user", ttl)
	after := time.Now()
	if err != nil {
		t.Fatalf("SignDevToken() unexpected error: %v", err)
	}

	tok, _ := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	var std josejwt.Claims
	_ = tok.Claims(&kp.PrivateKey.PublicKey, &std)

	expTime := std.Expiry.Time()
	lo := before.Add(ttl).Add(-2 * time.Second)
	hi := after.Add(ttl).Add(2 * time.Second)
	if expTime.Before(lo) || expTime.After(hi) {
		t.Errorf("expiry %v not in expected window [%v, %v]", expTime, lo, hi)
	}
}

// ─── vibewarden token command (integration) ───────────────────────────────────

func TestTokenCmd_DefaultOutput(t *testing.T) {
	dir, _ := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"token", "--key-dir", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	// First line must be the JWT (starts with "ey").
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "ey") {
		t.Errorf("first output line does not look like a JWT: %q", lines[0])
	}

	// Non-JSON output must contain usage hints.
	if !strings.Contains(out, "Hint:") {
		t.Errorf("default output should contain a usage hint, got:\n%s", out)
	}
}

func TestTokenCmd_JSONOutput(t *testing.T) {
	dir, _ := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"token", "--key-dir", dir, "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	token := strings.TrimSpace(outBuf.String())
	if !strings.HasPrefix(token, "ey") {
		t.Errorf("--json output does not look like a JWT: %q", token)
	}
	// Must be a single line (no hints).
	if strings.Contains(token, "\n") {
		t.Errorf("--json output contains newlines: %q", token)
	}
}

func TestTokenCmd_CustomClaims(t *testing.T) {
	dir, kp := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{
		"token", "--key-dir", dir, "--json",
		"--sub", "user-42",
		"--email", "alice@example.com",
		"--name", "Alice",
		"--role", "admin",
		"--expires", "30m",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	raw := strings.TrimSpace(outBuf.String())
	tok, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() unexpected error: %v", err)
	}

	var std josejwt.Claims
	var custom map[string]any
	if err := tok.Claims(&kp.PrivateKey.PublicKey, &std, &custom); err != nil {
		t.Fatalf("Claims() unexpected error: %v", err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"sub", std.Subject, "user-42"},
		{"email", custom["email"], "alice@example.com"},
		{"name", custom["name"], "Alice"},
		{"role", custom["role"], "admin"},
	}
	for _, c := range checks {
		t.Run(c.field, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("claim %s = %v, want %v", c.field, c.got, c.want)
			}
		})
	}
}

func TestTokenCmd_MissingKeys(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"token", "--key-dir", "/nonexistent/path/dev-keys"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for missing keys, got nil")
	}
}

func TestTokenCmd_InvalidExpires(t *testing.T) {
	dir, _ := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"token", "--key-dir", dir, "--expires", "notaduration"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for invalid --expires, got nil")
	}
}

func TestTokenCmd_ZeroExpires(t *testing.T) {
	dir, _ := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"token", "--key-dir", dir, "--expires", "0s"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for zero --expires, got nil")
	}
}

func TestTokenCmd_NegativeExpires(t *testing.T) {
	dir, _ := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"token", "--key-dir", dir, "--expires", "-1h"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for negative --expires, got nil")
	}
}

func TestTokenCmd_CorruptPrivateKey(t *testing.T) {
	dir := t.TempDir()
	// Write a syntactically invalid PEM file.
	badPEM := "-----BEGIN PRIVATE KEY-----\nnotbase64\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(dir, jwtadapter.DevPrivateKeyFile), []byte(badPEM), 0o600); err != nil {
		t.Fatalf("writing corrupt key: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs([]string{"token", "--key-dir", dir})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for corrupt key, got nil")
	}
}

func TestTokenCmd_ValidatesAgainstPublicKey(t *testing.T) {
	dir, kp := generateTestKeyPair(t)

	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"token", "--key-dir", dir, "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	raw := strings.TrimSpace(outBuf.String())

	// Attempt to verify with a different (wrong) RSA public key — must fail.
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating wrong key: %v", err)
	}

	tok, err := josejwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() unexpected error: %v", err)
	}

	var std josejwt.Claims
	if err := tok.Claims(&wrongKey.PublicKey, &std); err == nil {
		t.Error("Claims() with wrong key should have failed, but did not")
	}

	// Verify with the correct public key — must succeed.
	if err := tok.Claims(&kp.PrivateKey.PublicKey, &std); err != nil {
		t.Errorf("Claims() with correct key unexpected error: %v", err)
	}
}
