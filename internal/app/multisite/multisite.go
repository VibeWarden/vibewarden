// Package multisite provides shared detection logic for multi-site project
// layouts. A project is considered multi-site when at least one subdirectory
// of the sites/ directory (adjacent to the root vibewarden.yaml) contains its
// own vibewarden.yaml.
//
// This package is consumed by the validate, add, and bundle command surfaces
// to give users consistent early feedback when they attempt production-deploy
// operations on multi-site layouts that are not yet supported (post-v1, tracked
// at https://github.com/VibeWarden/vibewarden/issues/1169).
package multisite

import (
	"os"
	"path/filepath"
)

// IsProject reports whether the directory inferred from configPath is the root
// of a multi-site project. A project is multi-site iff at least one
// subdirectory of sites/ (sitting next to configPath) contains a readable
// vibewarden.yaml file.
//
// Detection mirrors internal/config/sites.LoadSites: an empty sites/, a
// sites/<name>/ with no YAML, or a sites/<name>/vibewarden.yaml that is
// unreadable are all treated as single-site — consistent with LoadSites
// silently skipping subdirectories that lack a vibewarden.yaml.
//
// configPath is the path to the root vibewarden.yaml (or any file inside the
// project root). Only the directory component is used.
func IsProject(configPath string) bool {
	sitesDir := filepath.Join(filepath.Dir(configPath), "sites")
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		siteYAML := filepath.Join(sitesDir, e.Name(), "vibewarden.yaml")
		info, statErr := os.Stat(siteYAML)
		if statErr != nil || info.IsDir() {
			continue
		}
		return true
	}
	return false
}
