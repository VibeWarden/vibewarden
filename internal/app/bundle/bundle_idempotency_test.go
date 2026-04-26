package bundle_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
)

func TestBundle_Idempotency_DotEnvPreserved(t *testing.T) {
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	opts := bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	}

	// First run: .env is written from template.
	if err := svc.Bundle(context.Background(), opts); err != nil {
		t.Fatalf("first Bundle() error = %v", err)
	}
	dotEnvPath := filepath.Join(outDir, ".env")
	originalDotEnv := append([]byte(nil), mem.files[dotEnvPath]...)
	if len(originalDotEnv) == 0 {
		t.Fatal(".env missing after first bundle")
	}

	// Simulate user edits.
	edited := []byte("VIBEWARDEN_APP_IMAGE=myproject-app:latest\nSTRIPE_KEY=sk_live_abc\n")
	if err := mem.WriteFile(dotEnvPath, edited, 0o600); err != nil {
		t.Fatalf("simulating user edit: %v", err)
	}

	samplePath := filepath.Join(outDir, "sample.env")
	originalSample := append([]byte(nil), mem.files[samplePath]...)

	// Second run without --overwrite: .env is preserved; sample.env is
	// regenerated identically (deterministic).
	if err := svc.Bundle(context.Background(), opts); err != nil {
		t.Fatalf("second Bundle() error = %v", err)
	}

	if !bytes.Equal(mem.files[dotEnvPath], edited) {
		t.Errorf(".env overwritten despite Overwrite=false\ngot:  %q\nwant: %q", mem.files[dotEnvPath], edited)
	}
	if !bytes.Equal(mem.files[samplePath], originalSample) {
		t.Errorf("sample.env regenerated with different content (non-deterministic)")
	}
}

func TestBundle_Idempotency_OverwriteReplacesDotEnv(t *testing.T) {
	// With --overwrite, the pre-run snapshot is discarded. The extras
	// pipeline then merges whatever the generator wrote (credentials) with
	// the bundle body. In this unit-test setup the fake generator does not
	// write a .env, so we simulate the generator's clobber by clearing the
	// mem store's .env between runs — that matches the real-world flow
	// where the generator always overwrites .env with fresh credentials.
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	base := bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	}

	// First run.
	if err := svc.Bundle(context.Background(), base); err != nil {
		t.Fatalf("first Bundle() error = %v", err)
	}
	dotEnvPath := filepath.Join(outDir, ".env")
	original := append([]byte(nil), mem.files[dotEnvPath]...)

	// User edits.
	edited := []byte("VIBEWARDEN_APP_IMAGE=foo\nSECRET=keepme\n")
	if err := mem.WriteFile(dotEnvPath, edited, 0o600); err != nil {
		t.Fatalf("simulating user edit: %v", err)
	}

	// Simulate the real generator's clobber: between runs the real
	// generator writes a fresh .env that does NOT contain SECRET=keepme.
	// In this fake setup we clear the file so the extras pipeline sees
	// the same "blank slate" the real generator leaves behind.
	mem.mu.Lock()
	delete(mem.files, dotEnvPath)
	mem.mu.Unlock()

	// Second run WITH --overwrite: .env replaced.
	opts := base
	opts.Overwrite = true
	if err := svc.Bundle(context.Background(), opts); err != nil {
		t.Fatalf("second Bundle() error = %v", err)
	}

	if bytes.Equal(mem.files[dotEnvPath], edited) {
		t.Errorf(".env still contains user edits despite --overwrite")
	}
	if !bytes.Equal(mem.files[dotEnvPath], original) {
		t.Errorf(".env content after --overwrite differs from first-run default")
	}
}

func TestBundle_Idempotency_ReadmeOverwritten(t *testing.T) {
	mem := newMemBundleFS()
	svc := bundleapp.NewService(&fakeExecutor{}, &fakeGenerator{}).
		WithBundleFS(mem)

	outDir := t.TempDir()
	opts := bundleapp.BundleOptions{
		Config:      minimalBundleCfg(),
		ConfigPath:  filepath.Join(t.TempDir(), "vibewarden.yaml"),
		ProjectName: "myproject",
		OutputDir:   outDir,
		SkipImage:   true,
	}
	if err := svc.Bundle(context.Background(), opts); err != nil {
		t.Fatalf("first Bundle() error = %v", err)
	}
	readmePath := filepath.Join(outDir, "README.md")
	tampered := []byte("# tampered\n")
	if err := mem.WriteFile(readmePath, tampered, 0o600); err != nil {
		t.Fatalf("tampering README.md: %v", err)
	}
	if err := svc.Bundle(context.Background(), opts); err != nil {
		t.Fatalf("second Bundle() error = %v", err)
	}
	if bytes.Contains(mem.files[readmePath], []byte("tampered")) {
		t.Errorf("README.md not regenerated on second run — tampered content survived")
	}
}
