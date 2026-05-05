// Package architecture_test — release-asset presence invariant (issue #1316).
//
// This file guards the content-authority pipeline established by issue #1316:
// llms-full.txt, llms.txt, and vibewarden.reference.yaml must exist at the repo
// root and be non-empty so that goreleaser can publish them as GitHub Release
// assets. If any of these files is absent or empty, the website (vibewarden.dev)
// will get a 404 when it curls the release at build time — reproducing the silent
// drift that bit v0.18.7.
//
// These are not generated files. They are checked-in content that must be present
// and non-empty for every release. This test ensures that no future housekeeping
// PR accidentally deletes or truncates them.
package architecture_test

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

// TestReleaseAssets_ContentAuthorityFilesExistAndNonEmpty asserts that the three
// content-authority files referenced in .goreleaser.yml's release.extra_files
// block (#1316) exist at the repo root and contain at least one byte.
func TestReleaseAssets_ContentAuthorityFilesExistAndNonEmpty(t *testing.T) {
	// Resolve repo root by locating a known package (internal/ports always exists)
	// and climbing two directories up: <root>/internal/ports → <root>/internal → <root>.
	sentinel, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", build.FindOnly)
	if err != nil {
		t.Fatalf("locating module root via internal/ports: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(sentinel.Dir))

	files := []string{
		"llms-full.txt",
		"llms.txt",
		"vibewarden.reference.yaml",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(repoRoot, name)
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("release-asset file %q missing from repo root: %v\n"+
					"(.goreleaser.yml publishes this file as a GitHub Release asset — "+
					"vibewarden.dev will 404 without it)", name, statErr)
			}
			if info.Size() == 0 {
				t.Errorf("release-asset file %q is empty (zero bytes).\n"+
					"goreleaser will upload an empty file as a release asset — "+
					"vibewarden.dev fetches this at build time and will serve stale content.",
					name)
			}
		})
	}
}
