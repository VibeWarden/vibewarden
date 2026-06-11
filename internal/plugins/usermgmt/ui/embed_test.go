package ui_test

import (
	"io/fs"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins/usermgmt/ui"
)

// TestUIFS_ContainsExpectedEntries verifies that the embedded UIFS contains
// the expected asset entries under the assets/ prefix.
func TestUIFS_ContainsExpectedEntries(t *testing.T) {
	expected := []string{
		"assets/index.html",
		"assets/app.js",
		"assets/styles.css",
		"assets/logo.png",
	}
	for _, path := range expected {
		t.Run(path, func(t *testing.T) {
			f, err := ui.UIFS.Open(path)
			if err != nil {
				t.Fatalf("UIFS.Open(%q) error = %v; embedded asset missing from binary", path, err)
			}
			_ = f.Close()
		})
	}
}

// TestAssets_SubTreeContainsExpectedEntries verifies that Assets() returns an
// fs.FS where the top-level paths are "index.html", "app.js", "styles.css"
// (the assets/ prefix is stripped).
func TestAssets_SubTreeContainsExpectedEntries(t *testing.T) {
	sub := ui.Assets()
	expected := []string{"index.html", "app.js", "styles.css", "logo.png"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			f, err := sub.Open(name)
			if err != nil {
				t.Fatalf("Assets().Open(%q) error = %v", name, err)
			}
			_ = f.Close()
		})
	}
}

// TestAssets_FilesAreNonEmpty verifies that each embedded asset has non-zero
// content, catching accidental empty-file commits.
func TestAssets_FilesAreNonEmpty(t *testing.T) {
	sub := ui.Assets()
	files := []string{"index.html", "app.js", "styles.css", "logo.png"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			info, err := fs.Stat(sub, name)
			if err != nil {
				t.Fatalf("fs.Stat(%q) error = %v", name, err)
			}
			if info.Size() == 0 {
				t.Errorf("asset %q is empty (size = 0)", name)
			}
		})
	}
}
