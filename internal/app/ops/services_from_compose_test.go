package ops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// TestServicesFromComposeFile verifies that the helper correctly parses the
// services: keys from a compose file and returns them sorted.
func TestServicesFromComposeFile(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    []string
		wantErr bool
	}{
		{
			name:    "golden fixture with three services",
			path:    filepath.Join("testdata", "docker-compose.yml"),
			want:    []string{"app", "kratos", "vibewarden"},
			wantErr: false,
		},
		{
			name:    "file not found",
			path:    filepath.Join("testdata", "nonexistent.yml"),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ops.ServicesFromComposeFile(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ServicesFromComposeFile(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("services[%d]: got %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// TestServicesFromComposeFile_InvalidYAML verifies that invalid YAML returns
// an error.
func TestServicesFromComposeFile_InvalidYAML(t *testing.T) {
	// Write an invalid YAML file to a temp dir.
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "bad.yml")

	// Write invalid YAML content.
	if err := os.WriteFile(invalidPath, []byte("services: {\ninvalid yaml"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	_, err := ops.ServicesFromComposeFile(invalidPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// TestServicesFromComposeFile_Empty verifies that a compose file with no
// services returns an empty slice.
func TestServicesFromComposeFile_Empty(t *testing.T) {
	dir := t.TempDir()
	emptyComposePath := filepath.Join(dir, "docker-compose.yml")

	if err := os.WriteFile(emptyComposePath, []byte("name: empty\n"), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	got, err := ops.ServicesFromComposeFile(emptyComposePath)
	if err != nil {
		t.Fatalf("ServicesFromComposeFile() unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}
