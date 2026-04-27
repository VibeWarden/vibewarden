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

	// Positive: must contain active tls block.
	t.Run("has_active_tls_block", func(t *testing.T) {
		if !strings.Contains(content, "tls:") {
			t.Errorf("rendered production.yaml missing active 'tls:' key\n\nContent:\n%s", content)
		}
		if !strings.Contains(content, "enabled: true") {
			t.Errorf("rendered production.yaml missing 'enabled: true'\n\nContent:\n%s", content)
		}
		if !strings.Contains(content, "provider: letsencrypt") {
			t.Errorf("rendered production.yaml missing 'provider: letsencrypt'\n\nContent:\n%s", content)
		}
	})

	// Positive: must contain the overrides prompt line.
	t.Run("has_overrides_prompt", func(t *testing.T) {
		if !strings.Contains(content, "# Add overrides below.") {
			t.Errorf("rendered production.yaml missing '# Add overrides below.' prompt\n\nContent:\n%s", content)
		}
	})
}
