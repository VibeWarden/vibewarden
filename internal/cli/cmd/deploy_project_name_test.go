package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

func TestResolveProjectName(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		dirName    string
		want       string
	}{
		{
			name:       "explicit name wins",
			configYAML: "name: my-project\nserver:\n  port: 8443\nupstream:\n  port: 3000\n",
			dirName:    "some-dir",
			want:       "my-project",
		},
		{
			name:       "app.image used when name is empty",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\napp:\n  image: myapp:latest\n",
			dirName:    "some-dir",
			want:       "myapp",
		},
		{
			name:       "app.image without tag",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\napp:\n  image: myapp\n",
			dirName:    "some-dir",
			want:       "myapp",
		},
		{
			name:       "app.image with registry prefix",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\napp:\n  image: ghcr.io/org/myapp:v2\n",
			dirName:    "some-dir",
			want:       "myapp",
		},
		{
			name:       "directory fallback when both name and image are empty",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\n",
			dirName:    "cool-project",
			want:       "cool-project",
		},
		{
			name:       "name takes priority over app.image",
			configYAML: "name: explicit\nserver:\n  port: 8443\nupstream:\n  port: 3000\napp:\n  image: fromimage:latest\n",
			dirName:    "some-dir",
			want:       "explicit",
		},
		{
			name:       "config file missing falls back to directory name",
			configYAML: "", // empty means no file written
			dirName:    "fallback-dir",
			want:       "fallback-dir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tt.dirName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			var configPath string
			if tt.configYAML != "" {
				configPath = filepath.Join(dir, "vibewarden.yaml")
				if err := os.WriteFile(configPath, []byte(tt.configYAML), 0o644); err != nil {
					t.Fatalf("writing config: %v", err)
				}
			} else {
				// Point to a non-existent file inside the directory so the
				// directory-based fallback uses tt.dirName.
				configPath = filepath.Join(dir, "vibewarden.yaml")
			}

			got := resolveProjectName(configPath)
			if got != tt.want {
				t.Errorf("resolveProjectName(%q) = %q, want %q", configPath, got, tt.want)
			}
		})
	}
}

// TestResolveProjectName_MatchesDeploy verifies that the derivation chain in
// resolveProjectName produces the same result that the deploy command's inline
// logic would produce for the same config. This is the regression guard against
// the bug where status/logs diverged from deploy.
func TestResolveProjectName_MatchesDeploy(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		dirName    string
	}{
		{
			name:       "with explicit name",
			configYAML: "name: prod-app\nserver:\n  port: 8443\nupstream:\n  port: 3000\n",
			dirName:    "ignored-dir",
		},
		{
			name:       "with app.image only",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\napp:\n  image: webapp:latest\n",
			dirName:    "ignored-dir",
		},
		{
			name:       "with neither -- directory fallback",
			configYAML: "server:\n  port: 8443\nupstream:\n  port: 3000\n",
			dirName:    "my-directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tt.dirName)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			configPath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(configPath, []byte(tt.configYAML), 0o644); err != nil {
				t.Fatalf("writing config: %v", err)
			}

			fromHelper := resolveProjectName(configPath)

			// Simulate deploy's inline logic: load config, check Name,
			// check App.Image (with tag/registry stripping), then directory fallback.
			absConfig, _ := filepath.Abs(configPath)
			cfg, loadErr := config.Load(absConfig)

			var fromDeploy string
			if loadErr == nil && cfg.Name != "" {
				fromDeploy = cfg.Name
			}
			if fromDeploy == "" && loadErr == nil && cfg.App.Image != "" {
				image := cfg.App.Image
				if idx := strings.LastIndex(image, ":"); idx > 0 {
					image = image[:idx]
				}
				if idx := strings.LastIndex(image, "/"); idx >= 0 {
					image = image[idx+1:]
				}
				if image != "" {
					fromDeploy = image
				}
			}
			if fromDeploy == "" {
				fromDeploy = deployapp.ProjectNameFromConfig(absConfig)
			}

			if fromHelper != fromDeploy {
				t.Errorf("resolveProjectName and deploy logic diverge: helper=%q deploy=%q", fromHelper, fromDeploy)
			}
		})
	}
}
