package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prodWithComments is a hand-edited vibewarden.production.yaml that exercises
// every comment type the user is likely to add: top-of-file head, inline,
// blank lines, blocks, and commented-out stanzas.
const prodWithComments = `# production overrides — DO NOT DELETE COMMENTS BELOW
# managed by ops team
server:
  port: 443          # bind directly to 443

# TLS is managed here, not in vibewarden.yaml
tls:
  enabled: true
  provider: letsencrypt
  domain: old.example.com
  # storage_path is intentionally omitted — uses default

# auth is commented out on purpose, Kratos wiring is WIP
# auth:
#   mode: kratos
#   session_cookie_name: ory_kratos_session

# WAF stanza stays commented until block mode is approved
# waf:
#   enabled: true
#   mode: block
`

// TestAddTLSCmd_PreservesProductionYamlComments is the AC from issue #1086:
// running `vibew add tls` on a production.yaml that has hand-written
// comments and commented-out stanzas must leave every comment intact. Only
// the tls.domain leaf is allowed to change.
func TestAddTLSCmd_PreservesProductionYamlComments(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	// Set up base config (TLS already enabled — exercises the "do not touch
	// vibewarden.yaml" path in add tls).
	base := "tls:\n  enabled: true\n  provider: self-signed\n"
	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(base), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodWithComments), 0o600); err != nil {
		t.Fatalf("writing production.yaml: %v", err)
	}

	_, err := runAddCmd(t, dir, "tls", "--domain", "new.example.com")
	if err != nil {
		t.Fatalf("add tls: %v", err)
	}

	got, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	out := string(got)

	// Every hand-written comment must survive.
	mustSurvive := []string{
		"# production overrides — DO NOT DELETE COMMENTS BELOW",
		"# managed by ops team",
		"# bind directly to 443",
		"# TLS is managed here, not in vibewarden.yaml",
		"# storage_path is intentionally omitted — uses default",
		"# auth is commented out on purpose, Kratos wiring is WIP",
		"# auth:",
		"#   mode: kratos",
		"# WAF stanza stays commented until block mode is approved",
		"# waf:",
		"#   mode: block",
	}
	for _, c := range mustSurvive {
		if !strings.Contains(out, c) {
			t.Errorf("production.yaml lost comment %q\n\n--- got ---\n%s", c, out)
		}
	}

	// Only the domain changed.
	if !strings.Contains(out, "new.example.com") {
		t.Errorf("production.yaml missing new domain\n\n%s", out)
	}
	if strings.Contains(out, "old.example.com") {
		t.Errorf("production.yaml still contains old.example.com\n\n%s", out)
	}
}

// TestAddTLSCmd_RefusesOnBrokenProductionYaml asserts the "refuse on parse
// failure" requirement: if production.yaml is malformed, the command fails
// with a message pointing the user at `vibew validate` and the file is NOT
// overwritten.
func TestAddTLSCmd_RefusesOnBrokenProductionYaml(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("tls:\n  enabled: true\n  provider: self-signed\n"), 0o644); err != nil {
		t.Fatalf("writing vibewarden.yaml: %v", err)
	}

	broken := "server:\n  port: 443\n  bad: : :\n"
	prodPath := filepath.Join(dir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(broken), 0o600); err != nil {
		t.Fatalf("writing broken production.yaml: %v", err)
	}

	_, err := runAddCmd(t, dir, "tls", "--domain", "ignored.example.com")
	if err == nil {
		t.Fatal("expected error on broken production.yaml, got nil")
	}
	if !strings.Contains(err.Error(), "vibew validate") {
		t.Errorf("error should point to `vibew validate`, got: %v", err)
	}

	// File must be byte-identical to the broken fixture — no regeneration.
	got, _ := os.ReadFile(prodPath)
	if string(got) != broken {
		t.Errorf("broken production.yaml was silently modified:\n%s", got)
	}
}

// TestAddOtherCmds_DoNotTouchProductionYaml asserts that add_waf,
// add_ratelimit, add_metrics, add_auth, add_admin never read or write
// vibewarden.production.yaml. This is the "Toggler stays vibewarden.yaml-only"
// invariant from issue #1086.
func TestAddOtherCmds_DoNotTouchProductionYaml(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"add auth", []string{"auth"}},
		{"add rate-limiting", []string{"rate-limiting"}},
		{"add metrics", []string{"metrics"}},
		{"add admin", []string{"admin"}},
		{"add waf", []string{"waf"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := scaffoldTestDir(t, false)
			if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte(minimalVibeWardenYAML), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}

			prodPath := filepath.Join(dir, "vibewarden.production.yaml")
			prodContent := "# prod overrides — hand-edited\nserver:\n  port: 443\n"
			if err := os.WriteFile(prodPath, []byte(prodContent), 0o600); err != nil {
				t.Fatalf("writing prod: %v", err)
			}

			if _, err := runAddCmd(t, dir, tt.args...); err != nil {
				t.Fatalf("add %v: %v", tt.args, err)
			}

			got, _ := os.ReadFile(prodPath)
			if string(got) != prodContent {
				t.Errorf("production.yaml was modified by %v — it must not touch this file\n\n--- got ---\n%s", tt.args, got)
			}
		})
	}
}
