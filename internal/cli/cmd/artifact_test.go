package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// TestArtifact_AddTLS_ProdYAMLIsStructurallyComplete verifies that after
// `vibew add tls --domain`, vibewarden.production.yaml contains the three
// fields required for a non-placeholder `vibew bundle` output (#1336):
//   - profile: prod
//   - deploy.target_platform (non-empty → no bracketed placeholder in P3 check)
//   - tls.domain (non-empty → real domain in healthcheck URL)
//
// This is the artifact test that closes the cross-LLM literal-vs-template
// rule surfaced in the v0.18.4 retro: AI agents were copying SSH commands
// containing `<your-ssh-user>@<your-ssh-host>` verbatim.
func TestArtifact_AddTLS_ProdYAMLIsStructurallyComplete(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tlsMinimalBase), 0o644); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"add", "tls", "--domain", "demo.vibewarden.dev", "--email", "ops@vibewarden.dev", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("add tls failed: %v (output: %s)", err, out.String())
	}

	prodData, err := os.ReadFile(filepath.Join(dir, "vibewarden.production.yaml"))
	if err != nil {
		t.Fatalf("reading production.yaml: %v", err)
	}
	prod := string(prodData)

	for _, want := range []string{
		"profile: prod",
		"target_platform: linux/amd64",
		"domain: demo.vibewarden.dev",
	} {
		if !strings.Contains(prod, want) {
			t.Errorf("production.yaml missing %q (required to prevent bracketed placeholders in vibew bundle output)\n\n%s", want, prod)
		}
	}

	// deploy.host must NOT be present as an uncommented YAML key — its
	// absence is intentional and is the trigger for the hint paragraph in
	// `vibew bundle`. The operator must explicitly set it.
	for _, line := range strings.Split(prod, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "host:") {
			t.Errorf("production.yaml must NOT contain an uncommented 'host:' key — operator fills this in manually; got line: %q", line)
		}
	}
}

// TestArtifact_AddTLS_ProfileAndDeployWrittenToExistingProdYAML verifies that
// `vibew add tls` also writes profile: prod and deploy.target_platform to an
// existing vibewarden.production.yaml that was created without those fields
// (e.g. from a pre-#1336 run). This is the idempotent-upsert path.
func TestArtifact_AddTLS_ProfileAndDeployWrittenToExistingProdYAML(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tlsMinimalBase), 0o644); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Simulate a pre-#1336 production.yaml: only server.port and tls fields.
	existingProd := "server:\n  port: 443\ntls:\n  enabled: true\n  provider: letsencrypt\n  domain: old.example.com\n"
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.production.yaml"), []byte(existingProd), 0o644); err != nil {
		t.Fatalf("writing existing production.yaml: %v", err)
	}

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"add", "tls", "--domain", "new.example.com", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("add tls failed: %v (output: %s)", err, out.String())
	}

	prodData, err := os.ReadFile(filepath.Join(dir, "vibewarden.production.yaml"))
	if err != nil {
		t.Fatalf("reading production.yaml: %v", err)
	}
	prod := string(prodData)

	for _, want := range []string{
		"profile: prod",
		"target_platform: linux/amd64",
		"domain: new.example.com",
	} {
		if !strings.Contains(prod, want) {
			t.Errorf("production.yaml missing %q after upsert on existing file\n\n%s", want, prod)
		}
	}

	// Existing fields that were not touched must still be present.
	if !strings.Contains(prod, "provider: letsencrypt") {
		t.Errorf("production.yaml must still contain existing provider: letsencrypt\n\n%s", prod)
	}
}

// TestArtifact_AddTLS_BaseConfigUnchanged verifies that `vibew add tls --domain`
// does NOT modify vibewarden.yaml's tls.provider when it is already set to
// "self-signed". The domain should be written to vibewarden.production.yaml
// only; the base config's provider must remain unchanged.
//
// Regression test for #954.
func TestArtifact_AddTLS_BaseConfigUnchanged(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	// Create vibewarden.yaml with self-signed TLS.
	baseYAML := `server:
  port: 8443
tls:
  enabled: true
  provider: self-signed
upstream:
  host: "127.0.0.1"
  port: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Create vibewarden.production.yaml.
	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
`
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.production.yaml"), []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	// Run: vibew add tls --domain example.com <dir>
	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"add", "tls", "--domain", "example.com", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("add tls failed: %v", err)
	}

	// Read vibewarden.yaml — provider MUST still be "self-signed".
	baseData, err := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
	if err != nil {
		t.Fatalf("reading base config: %v", err)
	}
	baseStr := string(baseData)
	if !strings.Contains(baseStr, "provider: self-signed") && !strings.Contains(baseStr, "provider: \"self-signed\"") {
		t.Errorf("base config's tls.provider should still be 'self-signed', got:\n%s", baseStr)
	}

	// Read vibewarden.production.yaml — must contain the domain.
	prodData, err := os.ReadFile(filepath.Join(dir, "vibewarden.production.yaml"))
	if err != nil {
		t.Fatalf("reading prod config: %v", err)
	}
	prodStr := string(prodData)
	if !strings.Contains(prodStr, "example.com") {
		t.Errorf("production config should contain domain 'example.com', got:\n%s", prodStr)
	}
}

// TestArtifact_Init_GeneratesBothFiles verifies that `vibew init` creates both
// vibewarden.yaml and vibewarden.production.yaml with appropriate defaults.
// Regression guard for #1178: the production template must NOT contain a
// tls.provider: letsencrypt block — that caused vibew bundle to fail on fresh
// projects because tls.domain was not set. TLS is added via `vibew add tls`.
func TestArtifact_Init_GeneratesBothFiles(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "bothfiles")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// vibewarden.yaml must exist.
	if _, err := os.Stat(filepath.Join(projectDir, "vibewarden.yaml")); err != nil {
		t.Errorf("expected vibewarden.yaml to exist: %v", err)
	}

	// vibewarden.production.yaml must exist.
	prodPath := filepath.Join(projectDir, "vibewarden.production.yaml")
	if _, err := os.Stat(prodPath); err != nil {
		t.Errorf("expected vibewarden.production.yaml to exist: %v", err)
	}

	// vibewarden.production.yaml must NOT contain a tls block or letsencrypt
	// provider — the template was stripped in #1178. TLS is added explicitly
	// via `vibew add tls --domain ...` after the project is initialised.
	prodData, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("reading prod config: %v", err)
	}
	if strings.Contains(string(prodData), "letsencrypt") {
		t.Errorf("production config must NOT contain 'letsencrypt' (stale TLS block — #1178), got:\n%s", string(prodData))
	}
	if strings.Contains(string(prodData), "provider:") {
		t.Errorf("production config must NOT contain a 'provider:' key (stale TLS block — #1178), got:\n%s", string(prodData))
	}
}

// TestArtifact_Init_IgnoresHiddenDirs verifies that `vibew init` (without
// --force) does not fail when the only pre-existing entries in the directory
// are hidden directories like .claude/ and .git/. Hidden directories should
// be ignored in the "not empty" check.
//
// Regression test for #957.
func TestArtifact_Init_IgnoresHiddenDirs(t *testing.T) {
	parent := scaffoldTestDir(t, false)
	projectDir := filepath.Join(parent, "hidden")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create hidden directories that should be ignored.
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := cmd.NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init"})

	err = root.Execute()
	if err != nil {
		t.Errorf("init should succeed when directory only contains hidden dirs, got error: %v\noutput: %s", err, out.String())
	}
}

// TestArtifact_AddTLSPlusBundle_NoBracketedPlaceholders is the end-to-end
// regression gate for the v0.18.4 cross-LLM literal-vs-template retro (#1336).
//
// It chains three user steps:
//  1. vibew add tls --domain example.com --email test@example.com
//  2. Edit the generated vibewarden.production.yaml to set deploy.host
//     (the one manual step required before running vibew bundle).
//  3. vibew bundle --skip-image
//
// Then asserts that the bundle stdout SSH commands:
//   - DO NOT contain `<your-ssh-user>` or `<your-ssh-host>` bracketed placeholders.
//   - DO contain the real target `'root@198.51.100.10'` (RFC 5737 TEST-NET IP,
//     never resolvable — chosen to avoid accidental real-host semantics).
//
// This is the smoke-test friction class gate: a regression in the add-tls →
// bundle pipeline that reintroduces bracketed placeholders in SSH output will
// be caught here before it reaches a user.
func TestArtifact_AddTLSPlusBundle_NoBracketedPlaceholders(t *testing.T) {
	// Must NOT be t.Parallel(): os.Chdir is process-global.
	const testHost = "root@198.51.100.10" // RFC 5737 TEST-NET — never resolvable

	// Step 0: set up isolated project directory.
	dir := scaffoldTestDir(t, false)
	writeScaffoldingMarker(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(tlsMinimalBase), 0o644); err != nil {
		t.Fatalf("writing base config: %v", err)
	}

	// Step 1: vibew add tls --domain example.com --provider self-signed
	// Using self-signed avoids the ACME domain-validation requirement that
	// fires when the base config has tls.provider: letsencrypt but no domain.
	// The domain is still written to vibewarden.production.yaml; bundle reads it
	// from there via readProdTLSDomain to build the healthcheck URL.
	{
		root := cmd.NewRootCmd("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"add", "tls", "--domain", "example.com", "--provider", "self-signed", dir})
		if err := root.Execute(); err != nil {
			t.Fatalf("add tls failed: %v (output: %s)", err, out.String())
		}
	}

	// Step 2: programmatically set deploy.host in the generated
	// vibewarden.production.yaml using yamlmod.UpsertFields so comments and
	// ordering are preserved (same path as real operator tooling).
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if _, err := os.Stat(prodPath); err != nil {
		t.Fatalf("vibewarden.production.yaml not created by add tls: %v", err)
	}
	_, err := yamlmod.UpsertFields(prodPath, nil, func(root *yaml.Node, diff *yamlmod.DiffBuilder) error {
		yamlmod.UpsertScalar(root, diff, "deploy", "host", testHost, "!!str")
		return nil
	})
	if err != nil {
		t.Fatalf("yamlmod.UpsertFields (setting deploy.host): %v", err)
	}

	// Verify deploy.host is present before running bundle.
	prodData, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("reading production.yaml after host upsert: %v", err)
	}
	if !strings.Contains(string(prodData), testHost) {
		t.Fatalf("deploy.host not written to production.yaml; got:\n%s", string(prodData))
	}

	// Step 3: vibew bundle --skip-image (must chdir into project for bundle to
	// discover vibewarden.yaml and vibewarden.production.yaml from CWD).
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	outDir := filepath.Join(dir, "bundle-out")
	{
		root := cmd.NewRootCmd("test")
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stdout)
		root.SetArgs([]string{"bundle", "--skip-image", "--output", outDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("bundle failed: %v\nstdout:\n%s", err, stdout.String())
		}

		out := stdout.String()

		// Must NOT contain any bracketed placeholder in SSH command positions.
		for _, absent := range []string{"<your-ssh-user>", "<your-ssh-host>"} {
			if strings.Contains(out, absent) {
				t.Errorf("bundle stdout must NOT contain bracketed placeholder %q (v0.18.4 retro regression)\nstdout:\n%s", absent, out)
			}
		}

		// Must contain the real SSH target in single-quoted form (#1271 shell-quoting fix).
		wantSSHTarget := "'" + testHost + "'"
		if !strings.Contains(out, wantSSHTarget) {
			t.Errorf("bundle stdout must contain %q (real SSH target from deploy.host)\nstdout:\n%s", wantSSHTarget, out)
		}
	}

	// Optional: also assert the bundle README reflects the real host (not a placeholder).
	readmePath := filepath.Join(outDir, "README.md")
	if readmeData, readErr := os.ReadFile(readmePath); readErr == nil {
		readme := string(readmeData)
		for _, absent := range []string{"<your-ssh-user>", "<your-ssh-host>"} {
			if strings.Contains(readme, absent) {
				t.Errorf("bundle README.md must NOT contain bracketed placeholder %q\nREADME:\n%s", absent, readme)
			}
		}
		wantSSHTarget := "'" + testHost + "'"
		if !strings.Contains(readme, wantSSHTarget) {
			t.Errorf("bundle README.md must contain %q\nREADME:\n%s", wantSSHTarget, readme)
		}
	}
}
