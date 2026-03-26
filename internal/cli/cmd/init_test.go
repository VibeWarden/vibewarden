package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestNewInitCmd_FlagCombinations(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantErr       bool
		wantOutContains string
		setup         func(dir string)
		checkFiles    []string
	}{
		{
			name:          "defaults generate both files",
			args:          []string{},
			checkFiles:    []string{"vibewarden.yaml", "docker-compose.yml"},
			wantOutContains: "VibeWarden initialised successfully",
		},
		{
			name:       "skip-docker omits docker-compose",
			args:       []string{"--skip-docker"},
			checkFiles: []string{"vibewarden.yaml"},
		},
		{
			name:       "tls without domain returns error",
			args:       []string{"--tls"},
			wantErr:    true,
		},
		{
			name:       "tls with domain succeeds",
			args:       []string{"--tls", "--domain", "example.com"},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name:       "auth flag enabled",
			args:       []string{"--auth"},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name:       "rate-limit flag enabled",
			args:       []string{"--rate-limit"},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name:       "upstream port set",
			args:       []string{"--upstream", "8080"},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name:    "force flag overwrites existing files",
			args:    []string{"--force"},
			setup: func(dir string) {
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name: "existing vibewarden.yaml without force returns error",
			args: []string{},
			setup: func(dir string) {
				if err := os.WriteFile(filepath.Join(dir, "vibewarden.yaml"), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if tt.setup != nil {
				tt.setup(dir)
			}

			// Build the cobra root and add the init command.
			root := cmd.NewRootCmd("test")
			var outBuf bytes.Buffer
			root.SetOut(&outBuf)

			// Run: vibewarden init <dir> <flags...>
			allArgs := append([]string{"init", dir}, tt.args...)
			root.SetArgs(allArgs)

			err := root.Execute()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantOutContains != "" {
				if !strings.Contains(outBuf.String(), tt.wantOutContains) {
					t.Errorf("output %q does not contain %q", outBuf.String(), tt.wantOutContains)
				}
			}

			for _, filename := range tt.checkFiles {
				path := filepath.Join(dir, filename)
				if _, err := os.Stat(path); err != nil {
					t.Errorf("expected file %q to exist: %v", path, err)
				}
			}
		})
	}
}

func TestNewInitCmd_RenderedYAMLValid(t *testing.T) {
	// This test verifies that rendered vibewarden.yaml is non-empty and contains
	// expected keys — it does not fully parse YAML (that is tested in the
	// template adapter tests).
	tests := []struct {
		name         string
		args         []string
		wantInYAML   []string
	}{
		{
			name:       "default config contains server and upstream sections",
			args:       []string{},
			wantInYAML: []string{"server:", "upstream:", "port: 3000", "port: 8080"},
		},
		{
			name:       "auth flag adds kratos section",
			args:       []string{"--auth"},
			wantInYAML: []string{"kratos:", "public_url:"},
		},
		{
			name:       "rate-limit flag adds rate_limit section",
			args:       []string{"--rate-limit"},
			wantInYAML: []string{"rate_limit:", "per_ip:"},
		},
		{
			name:       "tls flag adds tls section with domain",
			args:       []string{"--tls", "--domain", "example.com"},
			wantInYAML: []string{"tls:", "enabled: true", "example.com"},
		},
		{
			name:       "upstream port appears in config",
			args:       []string{"--upstream", "4200"},
			wantInYAML: []string{"port: 4200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			root := cmd.NewRootCmd("test")
			allArgs := append([]string{"init", dir, "--skip-docker"}, tt.args...)
			root.SetArgs(allArgs)

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}

			yamlBytes, err := os.ReadFile(filepath.Join(dir, "vibewarden.yaml"))
			if err != nil {
				t.Fatalf("reading vibewarden.yaml: %v", err)
			}
			yamlContent := string(yamlBytes)

			for _, want := range tt.wantInYAML {
				if !strings.Contains(yamlContent, want) {
					t.Errorf("vibewarden.yaml does not contain %q\n\nContent:\n%s", want, yamlContent)
				}
			}
		})
	}
}
