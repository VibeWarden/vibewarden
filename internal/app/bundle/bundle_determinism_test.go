package bundle_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// TestBundle_Parity_RealGenerator_CoreFilesByteIdentical is the ADR-085 §4
// parity guard: `vibew deploy --dry-run` and `vibew bundle` must produce
// byte-identical docker-compose.yml AND vibewarden.yaml for the same input.
//
// The original parity test shelled out to fakeGenerator, which of course
// produced the same (empty) output in both paths. That told us nothing
// about real generation. The reviewer on PR #1061 flagged this: ADR-085 §4
// required byte equality on the REAL generator output for those two files.
// This rewrite instantiates the real templateadapter + credentialsadapter
// stack (the same stack wired by cmd/bundle.go and cmd/deploy.go) and runs
// both code paths against the exact same project fixture.
//
// Credentials and .env are randomised every run, so they stay out of the
// byte-identical set and are compared by file-set membership only — same
// treatment ADR-085 §4 gives them.
func TestBundle_Parity_RealGenerator_CoreFilesByteIdentical(t *testing.T) {
	// Realistic single-site fixture: base config + production override, same
	// shape as a `vibew init` project after the user sets a domain.
	projectDir := t.TempDir()
	baseYAML := `server:
  port: 8443
upstream:
  host: "0.0.0.0"
  port: 3000
tls:
  enabled: true
  provider: self-signed
rate_limit:
  enabled: true
  burst: 20
app:
  image: "myapp:latest"
`
	basePath := filepath.Join(projectDir, "vibewarden.yaml")
	if err := os.WriteFile(basePath, []byte(baseYAML), 0o600); err != nil {
		t.Fatalf("writing base config: %v", err)
	}
	prodYAML := `server:
  port: 443
tls:
  enabled: true
  provider: letsencrypt
  domain: "example.com"
  email: "ops@example.com"
`
	prodPath := filepath.Join(projectDir, "vibewarden.production.yaml")
	if err := os.WriteFile(prodPath, []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	cfg, err := bundleapp.LoadMergedConfig(basePath, prodPath)
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}

	// newRealService builds a bundleapp.Service backed by the production
	// adapter stack — the same wiring cmd/bundle.go and cmd/deploy.go use.
	// We construct it twice, once per invocation, so the two runs share zero
	// mutable state.
	newRealService := func() *bundleapp.Service {
		renderer := templateadapter.NewRenderer(configtemplates.FS)
		gen := generateapp.NewServiceWithCredentials(
			renderer,
			credentialsadapter.NewGenerator(),
			credentialsadapter.NewStore(),
		).WithConfigSourcePath(basePath)
		return bundleapp.NewService(&fakeExecutor{}, gen)
	}

	commonOpts := bundleapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     basePath,
		ProdConfigPath: prodPath,
		ProjectName:    "myproject",
		SkipImage:      true,
	}

	// Run A: mirrors `vibew deploy --dry-run` — no BundleFS wired, so the
	// extras pipeline is a no-op and only the core generator + merged YAML
	// are written.
	outA := t.TempDir()
	svcA := newRealService()
	optsA := commonOpts
	optsA.OutputDir = outA
	if err := svcA.Bundle(context.Background(), optsA); err != nil {
		t.Fatalf("dry-run Bundle() error = %v", err)
	}

	// Run B: mirrors `vibew bundle` — BundleFS wired, extras pipeline runs.
	// To keep parity, the extras pipeline goes to an in-memory FS so only
	// the two core files we care about end up on disk and can be byte-
	// compared. (A real bundle run would write everything to disk; the
	// byte-equality contract only applies to docker-compose.yml and
	// vibewarden.yaml, not to the extras.)
	outB := t.TempDir()
	mem := newMemBundleFS()
	svcB := newRealService().WithBundleFS(mem)
	optsB := commonOpts
	optsB.OutputDir = outB
	if err := svcB.Bundle(context.Background(), optsB); err != nil {
		t.Fatalf("bundle Bundle() error = %v", err)
	}

	// Byte-identical on the two core files — this is the ADR-085 §4 contract.
	for _, file := range []string{"docker-compose.yml", "vibewarden.yaml"} {
		a, err := os.ReadFile(filepath.Join(outA, file)) //nolint:gosec // test dir
		if err != nil {
			t.Fatalf("reading A/%s: %v", file, err)
		}
		b, err := os.ReadFile(filepath.Join(outB, file)) //nolint:gosec // test dir
		if err != nil {
			t.Fatalf("reading B/%s: %v", file, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s NOT byte-identical between `vibew deploy --dry-run` and `vibew bundle`\nA:\n%s\n\nB:\n%s", file, a, b)
		}
	}

	// Semantic equality on the rest: the file sets must match modulo the
	// ignore list. image.tar is SkipImage-ed, .credentials and .env carry
	// randomised bytes every run. Extras from run B live in the in-memory
	// FS and are expected to be absent from run A (the dry-run path does
	// not call writeBundleExtras because BundleFS is nil by design).
	ignore := map[string]bool{
		"image.tar":    true,
		".credentials": true,
		".env":         true,
		// Extras (vibew bundle only):
		"sample.env": true,
		"deploy.sh":  true,
		"README.md":  true,
	}
	setA := pruneSet(collectFileSet(t, outA), ignore)
	setB := pruneSet(collectFileSet(t, outB), ignore)
	sort.Strings(setA)
	sort.Strings(setB)
	if !reflect.DeepEqual(setA, setB) {
		t.Errorf("non-ignored file set mismatch\nA: %v\nB: %v", setA, setB)
	}
}

// collectFileSet returns the sorted list of files (relative paths) under dir.
func collectFileSet(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func pruneSet(in []string, ignore map[string]bool) []string {
	out := make([]string, 0, len(in))
	for _, f := range in {
		if ignore[f] {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
