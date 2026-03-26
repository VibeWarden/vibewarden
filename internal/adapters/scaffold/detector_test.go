package scaffold_test

import (
	"os"
	"path/filepath"
	"testing"

	scaffoldadapter "github.com/vibewarden/vibewarden/internal/adapters/scaffold"
	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestDetector_Detect(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(dir string) // creates files in dir
		wantType   scaffold.ProjectType
		wantPort   int
		wantDocker bool
		wantVW     bool
	}{
		{
			name: "node project with PORT in start script",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{
  "scripts": {
    "start": "PORT=4000 node index.js"
  }
}`)
			},
			wantType: scaffold.ProjectTypeNode,
			wantPort: 4000,
		},
		{
			name: "node project without PORT",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{
  "scripts": {
    "start": "node index.js"
  }
}`)
			},
			wantType: scaffold.ProjectTypeNode,
			wantPort: 0,
		},
		{
			name: "go project",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.22\n")
			},
			wantType: scaffold.ProjectTypeGo,
		},
		{
			name: "python project",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "requirements.txt"), "flask==3.0.0\n")
			},
			wantType: scaffold.ProjectTypePython,
		},
		{
			name:     "empty directory",
			setup:    func(dir string) {},
			wantType: scaffold.ProjectTypeUnknown,
		},
		{
			name: "docker-compose.yml detected",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'\n")
			},
			wantType:   scaffold.ProjectTypeUnknown,
			wantDocker: true,
		},
		{
			name: "docker-compose.yaml detected",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "docker-compose.yaml"), "version: '3'\n")
			},
			wantType:   scaffold.ProjectTypeUnknown,
			wantDocker: true,
		},
		{
			name: "vibewarden.yaml detected",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "vibewarden.yaml"), "server:\n  port: 8080\n")
			},
			wantType: scaffold.ProjectTypeUnknown,
			wantVW:   true,
		},
		{
			name: "node project PORT in dev script",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{
  "scripts": {
    "dev": "PORT=5173 vite"
  }
}`)
			},
			wantType: scaffold.ProjectTypeNode,
			wantPort: 5173,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			d := scaffoldadapter.NewDetector()
			got, err := d.Detect(dir)
			if err != nil {
				t.Fatalf("Detect() unexpected error: %v", err)
			}

			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.DetectedPort != tt.wantPort {
				t.Errorf("DetectedPort = %d, want %d", got.DetectedPort, tt.wantPort)
			}
			if got.HasDockerCompose != tt.wantDocker {
				t.Errorf("HasDockerCompose = %v, want %v", got.HasDockerCompose, tt.wantDocker)
			}
			if got.HasVibeWardenConfig != tt.wantVW {
				t.Errorf("HasVibeWardenConfig = %v, want %v", got.HasVibeWardenConfig, tt.wantVW)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}
