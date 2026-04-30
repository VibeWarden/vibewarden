package templates_test

import (
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// TestInitProject_ProductionYAML_NoCommentedStubs verifies that the rendered
// init-vibewarden.production.yaml.tmpl satisfies the acceptance criteria for
// #1145: fewer than 30 lines, no commented stub stanzas, and required active
// content present.
func TestInitProject_ProductionYAML_NoCommentedStubs(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	data := domainscaffold.InitProjectData{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "myapp",
	}

	rendered, err := renderer.Render("init-vibewarden.production.yaml.tmpl", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(rendered)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")

	// Positive: line count must be < 30.
	if len(lines) >= 30 {
		t.Errorf("rendered production.yaml has %d lines, want < 30\n\nContent:\n%s", len(lines), content)
	}

	// Negative: must not contain any commented stanza prefix.
	commentedPrefixes := []struct {
		name   string
		prefix string
	}{
		{"auth", "# auth:"},
		{"admin", "# admin:"},
		{"rate_limit", "# rate_limit:"},
		{"security_headers", "# security_headers:"},
		{"waf", "# waf:"},
		{"upstream", "# upstream:"},
		{"app", "# app:"},
		{"log", "# log:"},
		{"kratos", "# kratos:"},
	}

	for _, tt := range commentedPrefixes {
		t.Run("no_commented_"+tt.name, func(t *testing.T) {
			for _, line := range lines {
				if strings.TrimSpace(line) == tt.prefix || strings.HasPrefix(strings.TrimSpace(line), tt.prefix) {
					t.Errorf("rendered production.yaml contains forbidden commented stanza %q\n\nContent:\n%s", tt.prefix, content)
					break
				}
			}
		})
	}

	// Positive: must contain the yaml-language-server schema line.
	t.Run("has_schema_header", func(t *testing.T) {
		if !strings.Contains(content, "yaml-language-server: $schema") {
			t.Errorf("rendered production.yaml missing yaml-language-server schema header\n\nContent:\n%s", content)
		}
	})

	// Positive: must contain active server and port.
	t.Run("has_server_port_443", func(t *testing.T) {
		if !strings.Contains(content, "server:") {
			t.Errorf("rendered production.yaml missing active 'server:' key\n\nContent:\n%s", content)
		}
		if !strings.Contains(content, "port: 443") {
			t.Errorf("rendered production.yaml missing active 'port: 443'\n\nContent:\n%s", content)
		}
	})

	// Negative: must NOT contain any uncommented tls block or provider keys.
	// Regression guard for #1178: the template previously carried a stale
	// tls.provider: letsencrypt block that caused vibew bundle to fail on
	// fresh projects because tls.domain was not set.
	t.Run("no_tls_block", func(t *testing.T) {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Detect a top-level uncommented "tls:" key. We avoid matching
			// comment lines (# ...) and URLs that may contain "tls:" as a
			// substring. A line is a top-level tls: key when it is exactly
			// "tls:" (no leading indent, no comment prefix).
			if trimmed == "tls:" {
				t.Errorf("rendered production.yaml contains forbidden top-level 'tls:' key (stale TLS block — see #1178)\n\nContent:\n%s", content)
				break
			}
		}
		if strings.Contains(content, "provider: letsencrypt") {
			t.Errorf("rendered production.yaml contains forbidden 'provider: letsencrypt' (stale TLS block — see #1178)\n\nContent:\n%s", content)
		}
		if strings.Contains(content, "provider: acme") {
			t.Errorf("rendered production.yaml contains forbidden 'provider: acme' (stale TLS block — see #1178)\n\nContent:\n%s", content)
		}
		if strings.Contains(content, "enabled: true") {
			t.Errorf("rendered production.yaml contains forbidden 'enabled: true' (TLS block removed — see #1178)\n\nContent:\n%s", content)
		}
	})

	// Positive: must contain the overrides prompt line.
	t.Run("has_overrides_prompt", func(t *testing.T) {
		if !strings.Contains(content, "# Add overrides below.") {
			t.Errorf("rendered production.yaml missing '# Add overrides below.' prompt\n\nContent:\n%s", content)
		}
	})
}

// TestInitProject_ProductionYAML_HasDeployHostHint verifies that the rendered
// init-vibewarden.production.yaml.tmpl contains the commented-out deploy.host
// hint introduced in #1244. The hint must be a comment (not an active yaml key)
// so fresh projects are not accidentally broken by an unpopulated host field.
func TestInitProject_ProductionYAML_HasDeployHostHint(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	data := domainscaffold.InitProjectData{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "myapp",
	}

	rendered, err := renderer.Render("init-vibewarden.production.yaml.tmpl", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(rendered)

	// Must contain the commented hint somewhere.
	t.Run("has_deploy_host_hint", func(t *testing.T) {
		if !strings.Contains(content, "# host: user@host") {
			t.Errorf("rendered production.yaml missing deploy.host hint comment\n\nContent:\n%s", content)
		}
	})

	// The hint must be a comment (leading # after optional whitespace), not
	// an active yaml key. An active "host:" key would break strict validation
	// for users who never fill it in — empty value passes but "user@host"
	// would be forwarded verbatim as the SSH target.
	t.Run("host_hint_is_comment_not_active_key", func(t *testing.T) {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Active key: line starts with "host:" (no leading #).
			if trimmed == "host:" || strings.HasPrefix(trimmed, "host: ") {
				if !strings.HasPrefix(trimmed, "#") {
					t.Errorf("deploy.host appears as active yaml key — must be commented out\nLine: %q\n\nContent:\n%s", line, content)
				}
			}
		}
	})
}

// TestInitProject_ProductionYAML_HasDeployTargetPlatform verifies that the
// rendered init-vibewarden.production.yaml.tmpl contains deploy.target_platform
// as an actual yaml mapping (not a comment) — guard for #1200.
func TestInitProject_ProductionYAML_HasDeployTargetPlatform(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	data := domainscaffold.InitProjectData{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "myapp",
	}

	rendered, err := renderer.Render("init-vibewarden.production.yaml.tmpl", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(rendered)

	// Must contain deploy: block as a real yaml key (no leading #).
	t.Run("has_deploy_block", func(t *testing.T) {
		lines := strings.Split(content, "\n")
		found := false
		for _, line := range lines {
			// A top-level deploy: key: not indented, not a comment.
			if strings.TrimSpace(line) == "deploy:" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				// Verify it is truly uncommented (no leading # before deploy:).
				if !strings.HasPrefix(line, "#") {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("rendered production.yaml missing active 'deploy:' block\n\nContent:\n%s", content)
		}
	})

	// Must contain target_platform: linux/amd64 as an actual entry.
	t.Run("has_target_platform_amd64", func(t *testing.T) {
		if !strings.Contains(content, "target_platform: linux/amd64") {
			t.Errorf("rendered production.yaml missing 'target_platform: linux/amd64'\n\nContent:\n%s", content)
		}
		// Must not be a commented line.
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.Contains(line, "target_platform: linux/amd64") {
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					t.Errorf("target_platform: linux/amd64 is commented out — must be active yaml\n\nLine: %q", line)
				}
			}
		}
	})
}
