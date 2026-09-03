package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// stubRenderer is a ports.TemplateRenderer that writes a marker line for every
// template and optionally fails for one specific template name.
type stubRenderer struct {
	failOn string
}

func (s *stubRenderer) Render(templateName string, _ any) ([]byte, error) {
	if s.failOn != "" && s.failOn == templateName {
		return nil, errors.New("stub render failure")
	}
	return []byte("# rendered: " + templateName + "\n"), nil
}

func (s *stubRenderer) RenderToFile(templateName string, data any, path string, _ bool) error {
	rendered, err := s.Render(templateName, data)
	if err != nil {
		return fmt.Errorf("rendering %q: %w", templateName, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), permDir); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}
	return os.WriteFile(path, rendered, permConfig)
}

func TestResolveProjectRoot(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}

	tests := []struct {
		name             string
		configSourcePath string
		want             string
	}{
		{
			name:             "empty path falls back to the working directory",
			configSourcePath: "",
			want:             cwd,
		},
		{
			name:             "absolute path resolves to its parent directory",
			configSourcePath: filepath.Join(string(filepath.Separator), "srv", "app", "vibewarden.yaml"),
			want:             filepath.Join(string(filepath.Separator), "srv", "app"),
		},
		{
			name:             "relative path is made absolute against the working directory",
			configSourcePath: filepath.Join("sub", "dir", "vibewarden.yaml"),
			want:             filepath.Join(cwd, "sub", "dir"),
		},
		{
			name:             "bare filename resolves to the working directory",
			configSourcePath: "vibewarden.yaml",
			want:             cwd,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveProjectRoot(tt.configSourcePath)
			if err != nil {
				t.Fatalf("resolveProjectRoot(%q): %v", tt.configSourcePath, err)
			}
			if got != tt.want {
				t.Errorf("resolveProjectRoot(%q) = %q, want %q", tt.configSourcePath, got, tt.want)
			}
		})
	}
}

func TestWithConfigSourcePath(t *testing.T) {
	svc := NewService(&stubRenderer{})

	got := svc.WithConfigSourcePath("/srv/app/vibewarden.yaml")
	if got != svc {
		t.Errorf("WithConfigSourcePath returned %p, want the receiver %p", got, svc)
	}
	if svc.configSourcePath != "/srv/app/vibewarden.yaml" {
		t.Errorf("configSourcePath = %q, want %q", svc.configSourcePath, "/srv/app/vibewarden.yaml")
	}

	// Chaining a second call overwrites the first.
	svc.WithConfigSourcePath("/other/vibewarden.yaml")
	if svc.configSourcePath != "/other/vibewarden.yaml" {
		t.Errorf("configSourcePath = %q, want it overwritten", svc.configSourcePath)
	}
}

// TestWithConfigSourcePath_DrivesProjectRoot verifies the observable effect of
// the setter: the project root is the directory holding the source config, not
// the process working directory.
func TestWithConfigSourcePath_DrivesProjectRoot(t *testing.T) {
	projectDir := t.TempDir()
	srcPath := filepath.Join(projectDir, "vibewarden.yaml")
	if err := os.WriteFile(srcPath, []byte("profile: dev\n"), 0o600); err != nil {
		t.Fatalf("writing source config: %v", err)
	}

	svc := NewService(&stubRenderer{}).WithConfigSourcePath(srcPath)
	root, err := resolveProjectRoot(svc.configSourcePath)
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	// t.TempDir may hand back a symlinked path (/var -> /private/var on macOS),
	// so compare the evaluated forms.
	wantRoot, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("evaluating symlinks: %v", err)
	}
	gotRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("evaluating symlinks: %v", err)
	}
	if gotRoot != wantRoot {
		t.Errorf("project root = %q, want %q", gotRoot, wantRoot)
	}
}

func TestCopyVibewardenYAML(t *testing.T) {
	const body = "profile: dev\nserver:\n  port: 8080\n"

	t.Run("copies the explicit source into the output directory", func(t *testing.T) {
		srcDir := t.TempDir()
		src := filepath.Join(srcDir, "vibewarden.yaml")
		if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
			t.Fatalf("writing source: %v", err)
		}
		outputDir := t.TempDir()

		svc := NewService(&stubRenderer{}).WithConfigSourcePath(src)
		if err := svc.copyVibewardenYAML(&config.Config{}, outputDir); err != nil {
			t.Fatalf("copyVibewardenYAML: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(outputDir, "vibewarden.yaml"))
		if err != nil {
			t.Fatalf("reading copy: %v", err)
		}
		if string(got) != body {
			t.Errorf("copy content = %q, want %q", got, body)
		}
	})

	t.Run("falls back to vibewarden.yaml in the working directory", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "vibewarden.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("writing source: %v", err)
		}
		t.Chdir(workDir)
		outputDir := t.TempDir()

		svc := NewService(&stubRenderer{})
		if err := svc.copyVibewardenYAML(&config.Config{}, outputDir); err != nil {
			t.Fatalf("copyVibewardenYAML: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(outputDir, "vibewarden.yaml"))
		if err != nil {
			t.Fatalf("reading copy: %v", err)
		}
		if string(got) != body {
			t.Errorf("copy content = %q, want %q", got, body)
		}
	})

	t.Run("missing source is skipped silently", func(t *testing.T) {
		t.Chdir(t.TempDir())
		outputDir := t.TempDir()

		svc := NewService(&stubRenderer{})
		if err := svc.copyVibewardenYAML(&config.Config{}, outputDir); err != nil {
			t.Fatalf("copyVibewardenYAML with no source: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outputDir, "vibewarden.yaml")); !os.IsNotExist(err) {
			t.Errorf("expected no copy to be written, stat err = %v", err)
		}
	})

	t.Run("unreadable source is a hard error", func(t *testing.T) {
		srcDir := t.TempDir()
		// A directory named vibewarden.yaml: os.ReadFile fails with something
		// other than fs.ErrNotExist, so the error must be propagated.
		src := filepath.Join(srcDir, "vibewarden.yaml")
		if err := os.Mkdir(src, 0o755); err != nil {
			t.Fatalf("creating directory: %v", err)
		}

		svc := NewService(&stubRenderer{}).WithConfigSourcePath(src)
		err := svc.copyVibewardenYAML(&config.Config{}, t.TempDir())
		if err == nil {
			t.Fatal("copyVibewardenYAML with a directory source: want error, got nil")
		}
		if !strings.Contains(err.Error(), "reading") {
			t.Errorf("error = %v, want it to mention reading the source", err)
		}
	})

	t.Run("unwritable destination is a hard error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: directory permissions are not enforced")
		}
		srcDir := t.TempDir()
		src := filepath.Join(srcDir, "vibewarden.yaml")
		if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
			t.Fatalf("writing source: %v", err)
		}
		outputDir := filepath.Join(t.TempDir(), "readonly")
		if err := os.Mkdir(outputDir, 0o500); err != nil {
			t.Fatalf("creating read-only dir: %v", err)
		}
		// G302 flags 0o700 as too permissive for a file; this is a directory,
		// which needs the execute bit for t.TempDir cleanup to remove it.
		t.Cleanup(func() { _ = os.Chmod(outputDir, 0o700) }) //nolint:gosec // directory, not a file

		svc := NewService(&stubRenderer{}).WithConfigSourcePath(src)
		err := svc.copyVibewardenYAML(&config.Config{}, outputDir)
		if err == nil {
			t.Fatal("copyVibewardenYAML into a read-only directory: want error, got nil")
		}
		if !strings.Contains(err.Error(), "writing") {
			t.Errorf("error = %v, want it to mention writing the destination", err)
		}
	})
}

func TestGenerateObservability(t *testing.T) {
	t.Run("writes every observability config", func(t *testing.T) {
		outputDir := t.TempDir()
		svc := NewService(&stubRenderer{})
		if err := svc.generateObservability(&config.Config{}, outputDir); err != nil {
			t.Fatalf("generateObservability: %v", err)
		}

		want := []string{
			filepath.Join("prometheus", "prometheus.yml"),
			filepath.Join("grafana", "provisioning", "datasources", "datasources.yml"),
			filepath.Join("grafana", "provisioning", "dashboards", "dashboards.yml"),
			filepath.Join("grafana", "dashboards", "vibewarden.json"),
			filepath.Join("loki", "loki-config.yml"),
			filepath.Join("promtail", "promtail-config.yml"),
			filepath.Join("otel-collector", "config.yaml"),
		}
		for _, rel := range want {
			p := filepath.Join(outputDir, "observability", rel)
			info, err := os.Stat(p)
			if err != nil {
				t.Errorf("expected %s to exist: %v", rel, err)
				continue
			}
			// Bind-mount sources must be regular files, never directories that
			// Docker Compose auto-created.
			if !info.Mode().IsRegular() {
				t.Errorf("%s is not a regular file (mode %v)", rel, info.Mode())
			}
			if info.Size() == 0 {
				t.Errorf("%s is empty", rel)
			}
		}
	})

	t.Run("propagates renderer failures with template context", func(t *testing.T) {
		tests := []struct {
			template string
			wantErr  string
		}{
			{"observability/prometheus.yml.tmpl", "rendering prometheus.yml"},
			{"observability/grafana-datasources.yml.tmpl", "rendering grafana datasources"},
			{"observability/grafana-dashboards.yml.tmpl", "rendering grafana dashboard provisioner"},
			{"observability/loki-config.yml.tmpl", "rendering loki-config.yml"},
			{"observability/promtail-config.yml.tmpl", "rendering promtail-config.yml"},
			{"observability/otel-collector-config.yml.tmpl", "rendering otel-collector config"},
		}
		for _, tt := range tests {
			t.Run(tt.template, func(t *testing.T) {
				svc := NewService(&stubRenderer{failOn: tt.template})
				err := svc.generateObservability(&config.Config{}, t.TempDir())
				if err == nil {
					t.Fatalf("generateObservability with failing %q: want error, got nil", tt.template)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("directory creation failure is reported", func(t *testing.T) {
		dir := t.TempDir()
		// outputDir is a regular file, so MkdirAll under it cannot succeed.
		outputDir := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(outputDir, []byte("x"), 0o600); err != nil {
			t.Fatalf("writing file: %v", err)
		}

		svc := NewService(&stubRenderer{})
		err := svc.generateObservability(&config.Config{}, outputDir)
		if err == nil {
			t.Fatal("generateObservability under a file: want error, got nil")
		}
		if !strings.Contains(err.Error(), "creating directory") {
			t.Errorf("error = %v, want it to mention creating a directory", err)
		}
	})
}

// internalMinimalConfig is the smallest config that puts Generate into
// kratos mode, so that the Kratos-specific error paths are reachable.
func internalMinimalConfig() *config.Config {
	return &config.Config{
		ProjectRoot: string(filepath.Separator) + "srv" + string(filepath.Separator) + "app",
		Server:      config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream:    config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		Auth: config.AuthConfig{
			Mode:           config.AuthModeKratos,
			IdentitySchema: "email_password",
		},
	}
}

// TestGenerate_ErrorPaths locks the failure wrapping of the Generate body: the
// caller must be told which artefact failed, not just that "generate" failed.
func TestGenerate_ErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		// mutate adjusts the config and returns the output directory to use.
		setup   func(t *testing.T, cfg *config.Config, svc *Service) string
		wantErr string
	}{
		{
			name: "missing kratos override",
			setup: func(t *testing.T, cfg *config.Config, _ *Service) string {
				cfg.Overrides.KratosConfig = filepath.Join(t.TempDir(), "absent-kratos.yml")
				return t.TempDir()
			},
			wantErr: "reading kratos override config",
		},
		{
			name: "missing compose override",
			setup: func(t *testing.T, cfg *config.Config, _ *Service) string {
				cfg.Overrides.ComposeFile = filepath.Join(t.TempDir(), "absent-compose.yml")
				return t.TempDir()
			},
			wantErr: "reading compose override config",
		},
		{
			name: "missing identity schema override",
			setup: func(t *testing.T, cfg *config.Config, _ *Service) string {
				cfg.Overrides.IdentitySchema = filepath.Join(t.TempDir(), "absent-schema.json")
				return t.TempDir()
			},
			wantErr: "resolving identity schema",
		},
		{
			name: "output directory cannot be created",
			setup: func(t *testing.T, _ *config.Config, _ *Service) string {
				blocker := filepath.Join(t.TempDir(), "not-a-dir")
				if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
					t.Fatalf("writing file: %v", err)
				}
				return filepath.Join(blocker, "generated")
			},
			wantErr: "creating output directories",
		},
		{
			name: "unreadable vibewarden.yaml source",
			setup: func(t *testing.T, _ *config.Config, svc *Service) string {
				srcDir := t.TempDir()
				src := filepath.Join(srcDir, "vibewarden.yaml")
				if err := os.Mkdir(src, 0o755); err != nil {
					t.Fatalf("creating directory: %v", err)
				}
				svc.WithConfigSourcePath(src)
				return t.TempDir()
			},
			wantErr: "copying vibewarden.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := internalMinimalConfig()
			svc := NewService(&stubRenderer{})
			outputDir := tt.setup(t, cfg, svc)

			err := svc.Generate(t.Context(), cfg.ToGeneratorInput(), outputDir)
			if err == nil {
				t.Fatal("Generate: want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
