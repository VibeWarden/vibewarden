package multisite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/multisite"
)

// TestIsProject covers the full detection matrix for multi-site project
// layouts. The detection logic mirrors internal/config/sites.LoadSites:
// only a subdirectory of sites/ that contains a readable vibewarden.yaml
// qualifies the project as multi-site.
func TestIsProject(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, root string)
		wantMulti bool
	}{
		{
			name:      "no sites dir",
			setup:     func(_ *testing.T, _ string) {},
			wantMulti: false,
		},
		{
			name: "empty sites dir",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites"), 0o750); err != nil {
					t.Fatalf("mkdir sites: %v", err)
				}
			},
			wantMulti: false,
		},
		{
			name: "sites/<name>/ with no vibewarden.yaml",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites", "blog"), 0o750); err != nil {
					t.Fatalf("mkdir sites/blog: %v", err)
				}
			},
			wantMulti: false,
		},
		{
			name: "sites/<name>/vibewarden.yaml present",
			setup: func(t *testing.T, root string) {
				t.Helper()
				siteDir := filepath.Join(root, "sites", "blog")
				if err := os.MkdirAll(siteDir, 0o750); err != nil {
					t.Fatalf("mkdir sites/blog: %v", err)
				}
				if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing site yaml: %v", err)
				}
			},
			wantMulti: true,
		},
		{
			name: "one empty site plus one populated site — populated wins",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "sites", "empty"), 0o750); err != nil {
					t.Fatalf("mkdir sites/empty: %v", err)
				}
				siteDir := filepath.Join(root, "sites", "shop")
				if err := os.MkdirAll(siteDir, 0o750); err != nil {
					t.Fatalf("mkdir sites/shop: %v", err)
				}
				if err := os.WriteFile(filepath.Join(siteDir, "vibewarden.yaml"), []byte("server:\n  port: 8443\n"), 0o600); err != nil {
					t.Fatalf("writing site yaml: %v", err)
				}
			},
			wantMulti: true,
		},
		{
			name: "sites/ contains a file (not a directory) named like a site",
			setup: func(t *testing.T, root string) {
				t.Helper()
				sitesDir := filepath.Join(root, "sites")
				if err := os.MkdirAll(sitesDir, 0o750); err != nil {
					t.Fatalf("mkdir sites: %v", err)
				}
				// A plain file at sites/notadir should not be treated as a site.
				if err := os.WriteFile(filepath.Join(sitesDir, "notadir"), []byte("data"), 0o600); err != nil {
					t.Fatalf("writing notadir: %v", err)
				}
			},
			wantMulti: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "vibewarden.yaml")
			// Write a stub root config so filepath.Dir works naturally.
			if err := os.WriteFile(configPath, []byte("server:\n  port: 8443\n"), 0o600); err != nil {
				t.Fatalf("writing root config: %v", err)
			}
			tt.setup(t, root)

			got := multisite.IsProject(configPath)
			if got != tt.wantMulti {
				t.Errorf("IsProject(%q) = %v, want %v", configPath, got, tt.wantMulti)
			}
		})
	}
}
