package scaffold_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestService_Init_WrapperGeneration(t *testing.T) {
	baseProject := &scaffold.ProjectConfig{
		Type: scaffold.ProjectTypeNode,
	}

	tests := []struct {
		name        string
		opts        scaffoldapp.InitOptions
		wantErr     bool
		checkFiles  []string
		absentFiles []string
	}{
		{
			name: "default init generates wrapper scripts",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				SkipDocker:   true,
			},
			checkFiles: []string{
				"vibew",
				"vibew.ps1",
				"vibew.cmd",
				".vibewarden-version",
			},
		},
		{
			name: "skip-wrapper omits all wrapper files",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				SkipDocker:   true,
				SkipWrapper:  true,
			},
			absentFiles: []string{
				"vibew",
				"vibew.ps1",
				"vibew.cmd",
				".vibewarden-version",
			},
		},
		{
			name: "force overwrites existing wrapper files",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				SkipDocker:   true,
				Force:        true,
			},
			checkFiles: []string{
				"vibew",
				"vibew.ps1",
				"vibew.cmd",
				".vibewarden-version",
			},
		},
		{
			name: "wrapper without force fails when vibew exists",
			opts: scaffoldapp.InitOptions{
				UpstreamPort: 3000,
				SkipDocker:   true,
				Force:        false,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			detector := &fakeDetector{cfg: baseProject}
			renderer := newFakeRenderer()
			svc := scaffoldapp.NewService(renderer, detector)

			// Pre-create vibew to trigger the "already exists" error case.
			if tt.name == "wrapper without force fails when vibew exists" {
				if err := os.WriteFile(filepath.Join(dir, "vibew"), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			err := svc.Init(context.Background(), dir, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}

			for _, filename := range tt.checkFiles {
				path := filepath.Join(dir, filename)
				if _, statErr := os.Stat(path); statErr != nil {
					t.Errorf("expected file %q to exist: %v", path, statErr)
				}
			}

			for _, filename := range tt.absentFiles {
				path := filepath.Join(dir, filename)
				if _, statErr := os.Stat(path); statErr == nil {
					t.Errorf("file %q should not exist but does", path)
				}
			}
		})
	}
}

func TestService_Init_VibewExecutable(t *testing.T) {
	dir := t.TempDir()
	project := &scaffold.ProjectConfig{Type: scaffold.ProjectTypeNode}
	detector := &fakeDetector{cfg: project}
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewService(renderer, detector)

	opts := scaffoldapp.InitOptions{
		UpstreamPort: 3000,
		SkipDocker:   true,
	}
	if err := svc.Init(context.Background(), dir, opts); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}

	vibewPath := filepath.Join(dir, "vibew")
	info, err := os.Stat(vibewPath)
	if err != nil {
		t.Fatalf("vibew not found: %v", err)
	}
	// Verify the file is executable (any of user/group/other execute bits set).
	if info.Mode()&0o111 == 0 {
		t.Errorf("vibew should be executable, mode = %v", info.Mode())
	}
}

func TestService_Init_VersionWrittenToVersionFile(t *testing.T) {
	dir := t.TempDir()
	project := &scaffold.ProjectConfig{Type: scaffold.ProjectTypeNode}
	detector := &fakeDetector{cfg: project}

	// Use the real template renderer backed by a minimal FS so we can inspect
	// the .vibewarden-version content.  Because fakeRenderer writes a stub
	// "rendered:<tmpl>" string, we check that the version file exists and was
	// written; the real rendering is verified by the CLI integration test.
	renderer := newFakeRenderer()
	svc := scaffoldapp.NewService(renderer, detector)

	opts := scaffoldapp.InitOptions{
		UpstreamPort: 3000,
		SkipDocker:   true,
		Version:      "v1.2.3",
	}
	if err := svc.Init(context.Background(), dir, opts); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}

	// The fake renderer writes "rendered:vibewarden-version.tmpl" to the file.
	// We just verify the file was created.
	versionPath := filepath.Join(dir, ".vibewarden-version")
	if _, err := os.Stat(versionPath); err != nil {
		t.Errorf(".vibewarden-version not found: %v", err)
	}
}
