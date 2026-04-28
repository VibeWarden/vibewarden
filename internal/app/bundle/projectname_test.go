package bundle_test

import (
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestSanitiseProjectName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"all safe chars", "myapp-v2_dev", "myapp-v2_dev"},
		{"mixed case", "MyApp", "MyApp"},
		{"spaces become empty", "my app", "myapp"},
		{"dots stripped", "my.app", "myapp"},
		{"shell metacharacters stripped", `myproject" && rm -rf /`, "myprojectrm-rf"},
		{"empty input", "", ""},
		{"all unsafe", "!@#$%", ""},
		{"underscores and dashes kept", "my_app-v1", "my_app-v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bundleapp.SanitiseProjectName(tt.input)
			if got != tt.want {
				t.Errorf("SanitiseProjectName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeriveProjectName(t *testing.T) {
	tests := []struct {
		name        string
		cfgName     string
		projectRoot string
		want        string
	}{
		{
			name:        "explicit name takes precedence",
			cfgName:     "myapp",
			projectRoot: "/home/user/other",
			want:        "myapp",
		},
		{
			name:        "dirname fallback when name is empty",
			cfgName:     "",
			projectRoot: "/home/user/myproject",
			want:        "myproject",
		},
		{
			// Since v0.19.0 (#1199), ComposeProjectName() applies sanitizeProjectName
			// to cfg.Name (branch 1), which lowercases and replaces non-alnum chars
			// with hyphens. SanitiseProjectName then passes those hyphens through.
			// The result has run-on hyphens for each special char; they are valid
			// in Docker Compose project names ([a-z0-9_-]+).
			name:        "adversarial name is sanitised",
			cfgName:     `myproject" && rm -rf /`,
			projectRoot: "",
			want:        "myproject-----rm--rf",
		},
		{
			// sanitizeProjectName (config layer) lowercases and replaces
			// non-alnum with hyphens; SanitiseProjectName (bundle layer)
			// then strips hyphens if they are the only chars, but preserves
			// alnum and underscores. "My Cool App!" → "my-cool-app" via
			// sanitizeProjectName → "my-cool-app" via SanitiseProjectName.
			name:        "dirname sanitised",
			cfgName:     "",
			projectRoot: "/home/user/My Cool App!",
			want:        "my-cool-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Name:        tt.cfgName,
				ProjectRoot: tt.projectRoot,
			}
			got := bundleapp.DeriveProjectName(cfg, "")
			if got != tt.want {
				t.Errorf("DeriveProjectName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDeriveProjectName_MatchesComposeProjectName verifies that DeriveProjectName
// returns the same value as SanitiseProjectName(cfg.ComposeProjectName()) for
// all inputs — the two resolvers must be byte-equal (ADR-093, #1199).
func TestDeriveProjectName_MatchesComposeProjectName(t *testing.T) {
	tests := []struct {
		name        string
		cfgName     string
		projectRoot string
	}{
		{"name set", "myapp", "/home/user/other"},
		{"name empty, root set", "", "/home/user/myproject"},
		{"both empty", "", ""},
		{"adversarial name", `foo" && rm -rf /`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Name:        tt.cfgName,
				ProjectRoot: tt.projectRoot,
			}
			got := bundleapp.DeriveProjectName(cfg, "")
			want := bundleapp.SanitiseProjectName(cfg.ComposeProjectName())
			if got != want {
				t.Errorf("DeriveProjectName() = %q, want SanitiseProjectName(ComposeProjectName()) = %q", got, want)
			}
		})
	}
}
