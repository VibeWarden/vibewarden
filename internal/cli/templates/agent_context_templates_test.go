package templates_test

import (
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestAgentContextTemplates_Claudemd(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	tests := []struct {
		name         string
		data         domainscaffold.AgentContextData
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "all features enabled",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     3000,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				RateLimitRPS:     10,
				AdminEnabled:     true,
			},
			wantContains: []string{
				"port 3000",
				"X-User-Id",
				"X-User-Email",
				"X-User-Verified",
				"vibewarden.yaml",
				"auth:",
				"public_paths",
				"10 requests/second",
				"TLS termination",
				"/_vibewarden/health",
				"/_vibewarden/admin",
				"X-Admin-Key",
			},
		},
		{
			name: "auth disabled excludes auth section",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     8080,
				AuthEnabled:      false,
				RateLimitEnabled: false,
				TLSEnabled:       false,
				RateLimitRPS:     10,
				AdminEnabled:     false,
			},
			wantAbsent: []string{
				"X-User-Id",
				"X-User-Email",
				"public_paths",
			},
			wantContains: []string{
				"port 8080",
				"/_vibewarden/health",
			},
		},
		{
			name: "rate limiting disabled excludes rate limit section",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     3000,
				AuthEnabled:      false,
				RateLimitEnabled: false,
				TLSEnabled:       false,
				RateLimitRPS:     10,
				AdminEnabled:     false,
			},
			wantAbsent: []string{
				"requests/second",
				"429",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderer.Render("claude.md.tmpl", tt.data)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			content := string(out)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf("claude.md.tmpl output missing %q\n\nContent:\n%s", want, content)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(content, absent) {
					t.Errorf("claude.md.tmpl output unexpectedly contains %q\n\nContent:\n%s", absent, content)
				}
			}
		})
	}
}

func TestAgentContextTemplates_CursorRules(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	tests := []struct {
		name         string
		data         domainscaffold.AgentContextData
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "all features enabled",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     3000,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				RateLimitRPS:     10,
				AdminEnabled:     true,
			},
			wantContains: []string{
				"port 3000",
				"X-User-Id",
				"X-User-Email",
				"X-User-Verified",
				"public_paths",
				"10 req/s",
				"DO NOT IMPLEMENT",
				"/_vibewarden/health",
				"X-Admin-Key",
			},
		},
		{
			name: "auth disabled excludes auth section",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     5000,
				AuthEnabled:      false,
				RateLimitEnabled: false,
				TLSEnabled:       false,
				RateLimitRPS:     10,
				AdminEnabled:     false,
			},
			wantAbsent: []string{
				"X-User-Id",
				"public_paths",
			},
			wantContains: []string{
				"port 5000",
				"/_vibewarden/health",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderer.Render("cursor-rules.tmpl", tt.data)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			content := string(out)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf("cursor-rules.tmpl output missing %q\n\nContent:\n%s", want, content)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(content, absent) {
					t.Errorf("cursor-rules.tmpl output unexpectedly contains %q\n\nContent:\n%s", absent, content)
				}
			}
		})
	}
}

func TestAgentContextTemplates_AgentsMd(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	tests := []struct {
		name         string
		data         domainscaffold.AgentContextData
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "all features enabled",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     3000,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				RateLimitRPS:     10,
				AdminEnabled:     true,
			},
			wantContains: []string{
				"port 3000",
				"X-User-Id",
				"X-User-Email",
				"X-User-Verified",
				"public_paths",
				"10 requests/second",
				"429 Too Many Requests",
				"plain HTTP",
				"/_vibewarden/health",
				"/_vibewarden/admin",
				"X-Admin-Key",
				"vibewarden.dev/docs/config",
			},
		},
		{
			name: "auth disabled excludes auth section",
			data: domainscaffold.AgentContextData{
				UpstreamPort:     4000,
				AuthEnabled:      false,
				RateLimitEnabled: false,
				TLSEnabled:       false,
				RateLimitRPS:     10,
				AdminEnabled:     false,
			},
			wantAbsent: []string{
				"X-User-Id",
				"public_paths",
				"429",
				"plain HTTP",
				"/_vibewarden/admin",
			},
			wantContains: []string{
				"port 4000",
				"/_vibewarden/health",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderer.Render("agents.md.tmpl", tt.data)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			content := string(out)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf("agents.md.tmpl output missing %q\n\nContent:\n%s", want, content)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(content, absent) {
					t.Errorf("agents.md.tmpl output unexpectedly contains %q\n\nContent:\n%s", absent, content)
				}
			}
		})
	}
}
