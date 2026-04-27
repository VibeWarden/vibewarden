package bundle_test

import (
	"os"
	"path/filepath"
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
	dir := t.TempDir()
	configPath := filepath.Join(dir, "myproject", "vibewarden.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tests := []struct {
		name       string
		cfgName    string
		appImage   string
		configPath string
		want       string
	}{
		{
			name:       "explicit name takes precedence",
			cfgName:    "myapp",
			appImage:   "otherapp:latest",
			configPath: configPath,
			want:       "myapp",
		},
		{
			name:       "app.image used when name is empty",
			cfgName:    "",
			appImage:   "ghcr.io/org/webapp:v1.0",
			configPath: configPath,
			want:       "webapp",
		},
		{
			name:       "directory basename fallback",
			cfgName:    "",
			appImage:   "",
			configPath: configPath,
			want:       "myproject",
		},
		{
			name:       "adversarial name is sanitised",
			cfgName:    `myproject" && rm -rf /`,
			appImage:   "",
			configPath: configPath,
			want:       "myprojectrm-rf",
		},
		{
			name:       "image tag stripped",
			cfgName:    "",
			appImage:   "myapp:latest",
			configPath: configPath,
			want:       "myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Name: tt.cfgName,
				App: config.AppConfig{
					Image: tt.appImage,
				},
			}
			got := bundleapp.DeriveProjectName(cfg, tt.configPath)
			if got != tt.want {
				t.Errorf("DeriveProjectName() = %q, want %q", got, tt.want)
			}
		})
	}
}
