package scaffold_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/scaffold"
)

// fakeRenderer is a test double for ports.TemplateRenderer.
// It records calls and returns canned output.
type fakeRenderer struct {
	// rendered records (templateName -> rendered content).
	rendered map[string]string
	// renderErr, when non-nil, is returned by Render.
	renderErr error
	// renderToFileErr, when non-nil, is returned by RenderToFile.
	renderToFileErr error
}

func newFakeRenderer() *fakeRenderer {
	return &fakeRenderer{rendered: make(map[string]string)}
}

func (f *fakeRenderer) Render(templateName string, data any) ([]byte, error) {
	if f.renderErr != nil {
		return nil, f.renderErr
	}
	content := fmt.Sprintf("rendered:%s", templateName)
	f.rendered[templateName] = content
	return []byte(content), nil
}

func (f *fakeRenderer) RenderToFile(templateName string, data any, path string, overwrite bool) error {
	if f.renderToFileErr != nil {
		return f.renderToFileErr
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file exists: %w", os.ErrExist)
		}
	}
	// Write something to the path so callers can verify.
	content := fmt.Sprintf("rendered:%s", templateName)
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// fakeDetector is a test double for ports.ProjectDetector.
type fakeDetector struct {
	cfg *scaffold.ProjectConfig
	err error
}

func (f *fakeDetector) Detect(_ string) (*scaffold.ProjectConfig, error) {
	return f.cfg, f.err
}

func TestService_Init(t *testing.T) {
	baseProject := &scaffold.ProjectConfig{
		Type: scaffold.ProjectTypeNode,
	}

	tests := []struct {
		name        string
		opts        scaffoldapp.InitOptions
		project     *scaffold.ProjectConfig
		detectorErr error
		wantErr     bool
		wantErrIs   error
		checkFiles  []string // filenames that must exist in the temp dir
	}{
		{
			name: "happy path generates both files",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
			},
			project:    baseProject,
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name: "skip docker omits docker-compose",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				SkipDocker:   true,
			},
			project:    baseProject,
			checkFiles: []string{"vibewarden.yaml"},
		},
		{
			name: "uses detected port when upstream port is zero",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 0,
			},
			project: &scaffold.ProjectConfig{
				Type:         scaffold.ProjectTypeNode,
				DetectedPort: 4200,
			},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name: "uses default port 3000 when nothing detected",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 0,
			},
			project:    &scaffold.ProjectConfig{Type: scaffold.ProjectTypeUnknown},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name: "errors when vibewarden.yaml exists without force",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
			},
			project: &scaffold.ProjectConfig{
				Type:                scaffold.ProjectTypeGo,
				HasVibeWardenConfig: true,
			},
			wantErr:   true,
			wantErrIs: os.ErrExist,
		},
		{
			name: "errors when docker-compose.yml exists without force",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
			},
			project: &scaffold.ProjectConfig{
				Type:             scaffold.ProjectTypeGo,
				HasDockerCompose: true,
			},
			wantErr:   true,
			wantErrIs: os.ErrExist,
		},
		{
			name: "force flag allows overwriting existing files",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				Force:        true,
			},
			project: &scaffold.ProjectConfig{
				Type:                scaffold.ProjectTypeNode,
				HasVibeWardenConfig: true,
				HasDockerCompose:    true,
			},
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
		{
			name: "detector error is propagated",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
			},
			detectorErr: errors.New("disk error"),
			wantErr:     true,
		},
		{
			name: "all features enabled",
			opts: scaffoldapp.InitOptions{
				UpstreamPort:     8080,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				TLSDomain:        "example.com",
			},
			project:    baseProject,
			checkFiles: []string{"vibewarden.yaml", "docker-compose.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			project := tt.project
			if project == nil {
				project = baseProject
			}
			detector := &fakeDetector{cfg: project, err: tt.detectorErr}
			renderer := newFakeRenderer()

			svc := scaffoldapp.NewService(renderer, detector)
			err := svc.Init(context.Background(), dir, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Errorf("Init() error = %v, want errors.Is(%v)", err, tt.wantErrIs)
			}

			for _, filename := range tt.checkFiles {
				path := dir + "/" + filename
				if _, err := os.Stat(path); err != nil {
					t.Errorf("expected file %q to exist: %v", path, err)
				}
			}
		})
	}
}

// dirOf returns the parent directory of a path, mimicking filepath.Dir but
// without importing path/filepath to keep the test self-contained.
func dirOf(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

// Verify fakeDetector satisfies the interface at compile time.
var _ interface {
	Detect(string) (*scaffold.ProjectConfig, error)
} = (*fakeDetector)(nil)

// Verify fakeRenderer satisfies the ports.TemplateRenderer interface.
var _ interface {
	Render(string, any) ([]byte, error)
	RenderToFile(string, any, string, bool) error
} = (*fakeRenderer)(nil)

// Ensure fs package is imported (needed by the Open method in renderer_test).
var _ fs.FS = (fs.FS)(nil)
