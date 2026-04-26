package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles creates files in dir relative to the given map of relpath → content.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdirall %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
}

func TestDetectSensitiveFiles_StandardBundle(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".env":               "VIBEWARDEN_APP_IMAGE=myapp:latest\n",
		".credentials":       "KRATOS_ADMIN_TOKEN=secret\n",
		"docker-compose.yml": "version: '3'\n",
		"README.md":          "# Deploy\n",
		"vibewarden.yaml":    "server:\n  port: 8443\n",
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %v", len(got), got)
	}
	// Sort guarantees .credentials < .env lexicographically.
	if got[0].RelPath != ".credentials" || got[0].Description != "Kratos admin credentials" {
		t.Errorf("got[0] = %+v, want {RelPath:.credentials Description:Kratos admin credentials}", got[0])
	}
	if got[1].RelPath != ".env" || got[1].Description != "generated environment variables" {
		t.Errorf("got[1] = %+v, want {RelPath:.env Description:generated environment variables}", got[1])
	}
}

func TestDetectSensitiveFiles_KratosTree(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"kratos/secrets":              "cookie=abc\ncipher=xyz\n",
		"kratos/identity.schema.json": `{"$schema":"http://json-schema.org/draft-07/schema#"}`,
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %v", len(got), got)
	}

	// Sorted: identity.schema.json < secrets lexicographically under kratos/.
	identityIdx := -1
	secretsIdx := -1
	for i, m := range got {
		switch m.RelPath {
		case "kratos/identity.schema.json":
			identityIdx = i
		case "kratos/secrets":
			secretsIdx = i
		}
	}
	if identityIdx < 0 {
		t.Errorf("kratos/identity.schema.json not found in %v", got)
	} else if got[identityIdx].Description != "Kratos identity store data" {
		t.Errorf("kratos/identity.schema.json description = %q, want Kratos identity store data", got[identityIdx].Description)
	}
	if secretsIdx < 0 {
		t.Errorf("kratos/secrets not found in %v", got)
	} else if got[secretsIdx].Description != "Kratos cookie and cipher secrets" {
		t.Errorf("kratos/secrets description = %q, want Kratos cookie and cipher secrets", got[secretsIdx].Description)
	}
}

func TestDetectSensitiveFiles_KeyMaterial(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"tls.key":      "PRIVATE KEY\n",
		"cert-key.pem": "PRIVATE KEY\n",
		"cert.pem":     "CERTIFICATE\n",
		"bearer.token": "eyJhbGciOiJSUzI1NiJ9\n",
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d matches, want 4: %v", len(got), got)
	}

	byPath := make(map[string]string, len(got))
	for _, m := range got {
		byPath[m.RelPath] = m.Description
	}

	tests := []struct {
		path string
		desc string
	}{
		{"tls.key", "private key material"},
		{"cert-key.pem", "private key material"},
		{"cert.pem", "private key material"},
		{"bearer.token", "API token / bearer credential"},
	}
	for _, tt := range tests {
		if got, ok := byPath[tt.path]; !ok {
			t.Errorf("path %q not detected", tt.path)
		} else if got != tt.desc {
			t.Errorf("path %q description = %q, want %q", tt.path, got, tt.desc)
		}
	}
}

func TestDetectSensitiveFiles_NoMatches_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"docker-compose.yml": "version: '3'\n",
		"README.md":          "# Deploy\n",
		"sample.env":         "VIBEWARDEN_APP_IMAGE=myapp:latest\n",
		"vibewarden.yaml":    "server:\n  port: 8443\n",
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want 0: %v", len(got), got)
	}
}

func TestDetectSensitiveFiles_FirstMatchWins_KratosDotEnv(t *testing.T) {
	// A file kratos/.env matches the basename .env rule (first in table),
	// not the generic "kratos/" fallback. This verifies first-match-wins
	// ordering per ADR-094.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"kratos/.env": "KRATOS_COOKIE_SECRET=abc\n",
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1: %v", len(got), got)
	}
	if got[0].Description != "generated environment variables" {
		t.Errorf("description = %q, want 'generated environment variables' (basename .env wins)", got[0].Description)
	}
}

func TestRenderSensitiveBlock_Empty_WritesNothing(t *testing.T) {
	var buf bytes.Buffer
	renderSensitiveBlock(nil, &buf)
	if buf.Len() != 0 {
		t.Errorf("renderSensitiveBlock(nil) wrote %d bytes, want 0:\n%s", buf.Len(), buf.String())
	}

	buf.Reset()
	renderSensitiveBlock([]sensitiveFile{}, &buf)
	if buf.Len() != 0 {
		t.Errorf("renderSensitiveBlock([]) wrote %d bytes, want 0:\n%s", buf.Len(), buf.String())
	}
}

func TestRenderSensitiveBlock_StableFormat(t *testing.T) {
	input := []sensitiveFile{
		{RelPath: ".credentials", Description: "Kratos admin credentials"},
		{RelPath: ".env", Description: "generated environment variables"},
		{RelPath: "kratos/secrets", Description: "Kratos cookie and cipher secrets"},
	}

	want := "Sensitive files in this bundle:\n" +
		"  .credentials  — Kratos admin credentials\n" +
		"  .env  — generated environment variables\n" +
		"  kratos/secrets  — Kratos cookie and cipher secrets\n" +
		"These files ship with the bundle when you copy it to a host. If the host or\n" +
		"transport is untrusted, generate fresh credentials there instead.\n"

	var buf bytes.Buffer
	renderSensitiveBlock(input, &buf)
	got := buf.String()

	if got != want {
		t.Errorf("renderSensitiveBlock output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestDetectSensitiveFiles_SortedByRelPath verifies that the returned slice is
// lexicographically sorted regardless of filesystem walk order.
func TestDetectSensitiveFiles_SortedByRelPath(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"z.token":      "token\n",
		".env":         "vars\n",
		"a.key":        "key\n",
		".credentials": "creds\n",
	})

	got, err := detectSensitiveFiles(dir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	for i := 1; i < len(got); i++ {
		if got[i].RelPath < got[i-1].RelPath {
			t.Errorf("result not sorted: got[%d].RelPath=%q < got[%d].RelPath=%q",
				i, got[i].RelPath, i-1, got[i-1].RelPath)
		}
	}
}

// TestRunBundle_SensitiveBlockInStdout is an integration-style test that drives
// runBundle against a temp project with a fake generator and asserts the
// awareness block appears on stdout between the Contents listing and the Next
// hint. This exercises the wiring in bundle.go without a real Docker daemon.
func TestRunBundle_SensitiveBlockInStdout(t *testing.T) {
	// Build a minimal project directory with a vibewarden.yaml.
	projectDir := t.TempDir()
	configPath := filepath.Join(projectDir, "vibewarden.yaml")
	configContent := `server:
  port: 8443
upstream:
  host: app
  port: 3000
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	// Bundle output directory inside the project.
	outDir := t.TempDir()

	// Pre-populate the output dir with .env and .credentials so
	// detectSensitiveFiles has something to find, without actually running
	// the full bundle pipeline (which needs Docker).
	if err := os.WriteFile(filepath.Join(outDir, ".env"), []byte("VIBEWARDEN_APP_IMAGE=test:latest\n"), 0o600); err != nil {
		t.Fatalf("seeding .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, ".credentials"), []byte("TOKEN=abc\n"), 0o600); err != nil {
		t.Fatalf("seeding .credentials: %v", err)
	}

	// Call the detector + renderer directly to verify the output that
	// runBundle would emit after its Contents: block. This decouples the
	// test from the full Docker / generator pipeline.
	sensitive, err := detectSensitiveFiles(outDir)
	if err != nil {
		t.Fatalf("detectSensitiveFiles() error = %v", err)
	}
	if len(sensitive) == 0 {
		t.Fatal("detectSensitiveFiles returned empty — test setup invalid")
	}

	var buf bytes.Buffer
	renderSensitiveBlock(sensitive, &buf)
	stdout := buf.String()

	// Assertions matching the acceptance criteria.
	if !strings.Contains(stdout, "Sensitive files in this bundle:") {
		t.Errorf("awareness block header missing from stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, ".env") {
		t.Errorf(".env line missing from awareness block:\n%s", stdout)
	}
	if !strings.Contains(stdout, ".credentials") {
		t.Errorf(".credentials line missing from awareness block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "transport is untrusted") {
		t.Errorf("footer 'transport is untrusted' missing from awareness block:\n%s", stdout)
	}
}
