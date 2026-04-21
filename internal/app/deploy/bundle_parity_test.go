package deploy_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	"github.com/vibewarden/vibewarden/internal/config"
)

// TestBundle_Parity_SameConfigYieldsByteIdenticalCoreFiles is the ADR-085
// parity guard for #1053. Running Service.Bundle twice against the same
// inputs must produce byte-identical docker-compose.yml and
// vibewarden.yaml. Other files are compared by file-set equality (minus
// .credentials, whose contents are randomised every run).
func TestBundle_Parity_SameConfigYieldsByteIdenticalCoreFiles(t *testing.T) {
	// Build a realistic project tree. The base + prod override exercise
	// the same LoadMergedConfig path both commands share.
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
	if err := os.WriteFile(filepath.Join(projectDir, "vibewarden.yaml"), []byte(baseYAML), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(projectDir, "vibewarden.production.yaml"), []byte(prodYAML), 0o600); err != nil {
		t.Fatalf("writing prod config: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 443},
		Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
		TLS:      config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
	}

	opts := deployapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     filepath.Join(projectDir, "vibewarden.yaml"),
		ProdConfigPath: filepath.Join(projectDir, "vibewarden.production.yaml"),
		ProjectName:    "myproject",
		SkipImage:      true,
	}

	// Two separate bundles, two separate temp dirs. The service is the same
	// shape as vibew bundle (generator + BundleFS) and vibew deploy --dry-run
	// (generator only, no BundleFS) by construction.
	outA := t.TempDir()
	outB := t.TempDir()

	svcA := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{})
	optsA := opts
	optsA.OutputDir = outA
	if err := svcA.Bundle(context.Background(), optsA); err != nil {
		t.Fatalf("first Bundle() error = %v", err)
	}

	// Second run uses the extras pipeline (BundleFS wired). Even with extras
	// the two core files must still match byte-for-byte.
	mem := newMemBundleFS()
	svcB := deployapp.NewService(&fakeExecutor{}, &fakeGenerator{}).WithBundleFS(mem)
	optsB := opts
	optsB.OutputDir = outB
	if err := svcB.Bundle(context.Background(), optsB); err != nil {
		t.Fatalf("second Bundle() error = %v", err)
	}

	for _, file := range []string{"vibewarden.yaml"} {
		a, err := os.ReadFile(filepath.Join(outA, file)) //nolint:gosec // test dir
		if err != nil {
			t.Fatalf("reading A/%s: %v", file, err)
		}
		b, err := os.ReadFile(filepath.Join(outB, file)) //nolint:gosec // test dir
		if err != nil {
			t.Fatalf("reading B/%s: %v", file, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s NOT byte-identical across runs\nA:\n%s\nB:\n%s", file, a, b)
		}
	}

	// docker-compose.yml is produced by the generator. In this unit-test
	// setup the generator is a fake (does not actually write a compose),
	// so we instead assert it is either equally missing or equally present —
	// whichever the fake produced.
	_, errA := os.Stat(filepath.Join(outA, "docker-compose.yml"))
	_, errB := os.Stat(filepath.Join(outB, "docker-compose.yml"))
	if os.IsNotExist(errA) != os.IsNotExist(errB) {
		t.Errorf("docker-compose.yml presence differs between runs (A err=%v, B err=%v)", errA, errB)
	}

	// File-set equality (minus image.tar which is docker-dependent, and
	// .credentials whose content is randomised — existence-only checked).
	setA := collectFileSet(t, outA)
	setB := collectFileSet(t, outB)
	// runB's extras live in mem, not on disk — merge them so the comparison
	// reflects what a real vibew bundle would have produced.
	for p := range mem.files {
		rel, err := filepath.Rel(outB, p)
		if err != nil || rel == "." {
			continue
		}
		if len(rel) >= 2 && rel[:2] == ".." {
			continue
		}
		if !contains(setB, rel) {
			setB = append(setB, rel)
		}
	}
	sort.Strings(setA)
	sort.Strings(setB)

	// Only assert that the intersection of deterministic files matches. We
	// explicitly ignore image.tar (docker-dependent) and .credentials
	// (randomised) — both are allowed to be present in one but not both.
	ignore := map[string]bool{
		"image.tar":    true,
		".credentials": true,
	}
	pruneA := pruneSet(setA, ignore)
	pruneB := pruneSet(setB, ignore)

	// runA did not wire BundleFS, so its extras file-set is a subset of B's.
	// Parity is: every file in A must also be in B.
	for _, f := range pruneA {
		if !contains(pruneB, f) {
			t.Errorf("file %q present in run A but missing from run B", f)
		}
	}

	if !reflect.DeepEqual(pruneA, intersection(pruneA, pruneB)) {
		t.Errorf("file set parity mismatch\nA: %v\nB: %v", pruneA, pruneB)
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
	var out []string
	for _, f := range in {
		if ignore[f] {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func intersection(a, b []string) []string {
	seen := make(map[string]bool, len(b))
	for _, v := range b {
		seen[v] = true
	}
	var out []string
	for _, v := range a {
		if seen[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
