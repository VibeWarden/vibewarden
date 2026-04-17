package sites

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSites_NoDirectory(t *testing.T) {
	t.Parallel()

	// When the sites/ directory does not exist, LoadSites returns nil, nil
	// (backward-compatible single-app mode).
	sites, errs := LoadSites(filepath.Join(t.TempDir(), "nonexistent"))
	if sites != nil {
		t.Errorf("sites = %v, want nil", sites)
	}
	if errs != nil {
		t.Errorf("errs = %v, want nil", errs)
	}
}

func TestLoadSites_EmptyDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	sites, errs := LoadSites(sitesDir)
	if len(sites) != 0 {
		t.Errorf("len(sites) = %d, want 0", len(sites))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}

func TestLoadSites_SingleValidSite(t *testing.T) {
	t.Parallel()

	dir := setupSitesDir(t, map[string]string{
		"app1": validSiteYAML(),
	})

	sites, errs := LoadSites(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(sites) != 1 {
		t.Fatalf("len(sites) = %d, want 1", len(sites))
	}
	if sites[0].Name() != "app1" {
		t.Errorf("Name() = %q, want %q", sites[0].Name(), "app1")
	}
	if !sites[0].IsHealthy() {
		t.Errorf("IsHealthy() = false, want true")
	}
}

func TestLoadSites_MultipleValidSites(t *testing.T) {
	t.Parallel()

	dir := setupSitesDir(t, map[string]string{
		"app1": validSiteYAML(),
		"app2": validSiteYAML(),
		"app3": validSiteYAML(),
	})

	sites, errs := LoadSites(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(sites) != 3 {
		t.Fatalf("len(sites) = %d, want 3", len(sites))
	}

	names := map[string]bool{}
	for _, s := range sites {
		names[s.Name()] = true
	}
	for _, want := range []string{"app1", "app2", "app3"} {
		if !names[want] {
			t.Errorf("missing site %q in results", want)
		}
	}
}

func TestLoadSites_PartialSuccess(t *testing.T) {
	t.Parallel()

	// 3 sites: 2 valid, 1 with invalid YAML.
	dir := setupSitesDir(t, map[string]string{
		"good1":  validSiteYAML(),
		"broken": "{{{invalid yaml",
		"good2":  validSiteYAML(),
	})

	sites, errs := LoadSites(dir)

	// Should have errors from the broken site.
	if len(errs) == 0 {
		t.Fatal("expected at least one error for broken config")
	}

	healthy := 0
	errored := 0
	for _, s := range sites {
		if s.IsHealthy() {
			healthy++
		} else {
			errored++
		}
	}
	if healthy != 2 {
		t.Errorf("healthy sites = %d, want 2", healthy)
	}
	if errored != 1 {
		t.Errorf("error sites = %d, want 1", errored)
	}
}

func TestLoadSites_SubdirWithoutConfig(t *testing.T) {
	t.Parallel()

	// A subdirectory without vibewarden.yaml is silently skipped.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "empty-dir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	sites, errs := LoadSites(dir)
	if len(sites) != 0 {
		t.Errorf("len(sites) = %d, want 0", len(sites))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}

func TestLoadSites_FileInSitesDir(t *testing.T) {
	t.Parallel()

	// Non-directory entries in the sites dir are ignored.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stray-file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sites, errs := LoadSites(dir)
	if len(sites) != 0 {
		t.Errorf("len(sites) = %d, want 0", len(sites))
	}
	if len(errs) != 0 {
		t.Errorf("len(errs) = %d, want 0", len(errs))
	}
}

func TestLoadSites_InvalidDirectoryName(t *testing.T) {
	t.Parallel()

	// A directory with an uppercase name is not DNS-safe, so its Site
	// cannot be constructed. The error is recorded.
	dir := setupSitesDir(t, map[string]string{
		"InvalidName": validSiteYAML(),
	})

	sites, errs := LoadSites(dir)

	// The site should not appear in the list since its name is invalid.
	for _, s := range sites {
		if s.Name() == "InvalidName" {
			t.Error("site with invalid name should not appear in results")
		}
	}

	if len(errs) == 0 {
		t.Error("expected at least one error for invalid directory name")
	}
}

func TestLoadSites_MixedValidAndEmptyDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// One valid site.
	app1Dir := filepath.Join(dir, "app1")
	if err := os.Mkdir(app1Dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(app1Dir, "vibewarden.yaml"), []byte(validSiteYAML()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// One dir without config.
	if err := os.Mkdir(filepath.Join(dir, "no-config"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	sites, errs := LoadSites(dir)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(sites) != 1 {
		t.Errorf("len(sites) = %d, want 1", len(sites))
	}
}

// setupSitesDir creates a temporary directory structure:
//
//	<tmpdir>/
//	  <name>/vibewarden.yaml  (for each entry in configs)
func setupSitesDir(t *testing.T, configs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range configs {
		siteDir := filepath.Join(dir, name)
		if err := os.Mkdir(siteDir, 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", siteDir, err)
		}
		cfgPath := filepath.Join(siteDir, "vibewarden.yaml")
		if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", cfgPath, err)
		}
	}
	return dir
}

// validSiteYAML returns a minimal valid vibewarden.yaml that passes config.Load().
func validSiteYAML() string {
	return `server:
  host: "127.0.0.1"
  port: 8443
upstream:
  host: "127.0.0.1"
  port: 3000
`
}
