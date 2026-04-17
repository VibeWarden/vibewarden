package sites

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/site"
)

// TestIntegration_RealisticDirectoryTree loads a realistic multi-site
// directory layout: global.yaml + 3 site directories (2 valid, 1 broken),
// then populates a Registry and validates invariants.
func TestIntegration_RealisticDirectoryTree(t *testing.T) {
	t.Parallel()

	// Build directory tree:
	//   <root>/
	//     global.yaml
	//     sites/
	//       blog/vibewarden.yaml       (valid)
	//       api/vibewarden.yaml        (valid, different domain)
	//       broken/vibewarden.yaml     (invalid YAML)
	//       no-config/                 (no vibewarden.yaml — skipped)
	root := t.TempDir()

	// Global config.
	globalYAML := `admin_token: integration-test-token
listen_host: 0.0.0.0
listen_port: 443
log_level: info
acme_email: admin@example.com
`
	if err := os.WriteFile(filepath.Join(root, "global.yaml"), []byte(globalYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(global.yaml): %v", err)
	}

	// Sites directory.
	sitesDir := filepath.Join(root, "sites")
	if err := os.Mkdir(sitesDir, 0o755); err != nil {
		t.Fatalf("Mkdir(sites): %v", err)
	}

	blogYAML := `server:
  host: "127.0.0.1"
  port: 8443
upstream:
  host: "127.0.0.1"
  port: 3000
tls:
  domain: blog.example.com
`
	apiYAML := `server:
  host: "127.0.0.1"
  port: 8443
upstream:
  host: "127.0.0.1"
  port: 4000
tls:
  domain: api.example.com
`
	brokenYAML := `{{{not valid yaml`

	writeSiteConfig(t, sitesDir, "blog", blogYAML)
	writeSiteConfig(t, sitesDir, "api", apiYAML)
	writeSiteConfig(t, sitesDir, "broken", brokenYAML)

	// Dir without config.
	if err := os.Mkdir(filepath.Join(sitesDir, "no-config"), 0o755); err != nil {
		t.Fatalf("Mkdir(no-config): %v", err)
	}

	// --- Load global config ---
	gc, err := LoadGlobal(filepath.Join(root, "global.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobal() error = %v", err)
	}
	if gc.AdminToken != "integration-test-token" {
		t.Errorf("GlobalConfig.AdminToken = %q, want %q", gc.AdminToken, "integration-test-token")
	}
	if gc.ACMEEmail != "admin@example.com" {
		t.Errorf("GlobalConfig.ACMEEmail = %q, want %q", gc.ACMEEmail, "admin@example.com")
	}

	// --- Load sites ---
	sites, errs := LoadSites(sitesDir)

	// Expect errors from the broken site.
	if len(errs) == 0 {
		t.Fatal("expected at least one error for broken site")
	}

	// Count outcomes.
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

	// --- Populate registry ---
	registry := site.NewRegistry()
	registry.SetGlobal(*gc)

	for _, s := range sites {
		registry.Add(s)
	}

	// Registry should have all 3 sites (2 healthy + 1 error).
	if registry.Len() != 3 {
		t.Errorf("registry.Len() = %d, want 3", registry.Len())
	}

	// HealthySites should return only the 2 valid ones.
	hs := registry.HealthySites()
	if len(hs) != 2 {
		t.Errorf("HealthySites() = %d, want 2", len(hs))
	}

	// ErrorSites should return only the broken one.
	es := registry.ErrorSites()
	if len(es) != 1 {
		t.Errorf("ErrorSites() = %d, want 1", len(es))
	}
	if len(es) > 0 && es[0].Name() != "broken" {
		t.Errorf("error site name = %q, want %q", es[0].Name(), "broken")
	}

	// Domain validation should pass (blog.example.com and api.example.com are unique).
	if err := registry.ValidateDomains(); err != nil {
		t.Errorf("ValidateDomains() error = %v, want nil", err)
	}

	// Global config in registry should match.
	rg := registry.Global()
	if rg == nil {
		t.Fatal("registry.Global() = nil, want non-nil")
	}
	if rg.AdminToken != "integration-test-token" {
		t.Errorf("registry.Global().AdminToken = %q, want %q", rg.AdminToken, "integration-test-token")
	}
}

// TestIntegration_SingleAppBackwardCompat verifies that the absence of
// a sites/ directory (single-app mode) works correctly.
func TestIntegration_SingleAppBackwardCompat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// No global.yaml — should return defaults.
	gc, err := LoadGlobal(filepath.Join(root, "global.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobal() error = %v", err)
	}
	if gc.ListenHost != "0.0.0.0" {
		t.Errorf("ListenHost = %q, want default %q", gc.ListenHost, "0.0.0.0")
	}

	// No sites/ directory — returns nil, nil.
	sites, errs := LoadSites(filepath.Join(root, "sites"))
	if sites != nil {
		t.Errorf("sites = %v, want nil", sites)
	}
	if errs != nil {
		t.Errorf("errs = %v, want nil", errs)
	}
}

// TestIntegration_DuplicateDomainDetection verifies that the registry
// catches two sites claiming the same domain.
func TestIntegration_DuplicateDomainDetection(t *testing.T) {
	t.Parallel()

	sitesDir := t.TempDir()

	sameYAML := `server:
  host: "127.0.0.1"
  port: 8443
upstream:
  host: "127.0.0.1"
  port: 3000
tls:
  domain: same.example.com
`
	writeSiteConfig(t, sitesDir, "app1", sameYAML)
	writeSiteConfig(t, sitesDir, "app2", sameYAML)

	sites, errs := LoadSites(sitesDir)
	if len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	if len(sites) != 2 {
		t.Fatalf("len(sites) = %d, want 2", len(sites))
	}

	registry := site.NewRegistry()
	for _, s := range sites {
		registry.Add(s)
	}

	err := registry.ValidateDomains()
	if err == nil {
		t.Fatal("ValidateDomains() = nil, want error for duplicate domain")
	}
}

func writeSiteConfig(t *testing.T, sitesDir, name, content string) {
	t.Helper()
	dir := filepath.Join(sitesDir, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir(%s): %v", dir, err)
	}
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
