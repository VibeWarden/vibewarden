package bundlefs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vibewarden/vibewarden/internal/adapters/bundlefs"
)

func TestOSFS_Exists_FileMissing(t *testing.T) {
	fs := bundlefs.New()
	ok, err := fs.Exists(filepath.Join(t.TempDir(), "missing.txt"))
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}
	if ok {
		t.Errorf("Exists() = true, want false for missing path")
	}
}

func TestOSFS_Exists_FilePresent(t *testing.T) {
	fs := bundlefs.New()
	p := filepath.Join(t.TempDir(), "present.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}
	ok, err := fs.Exists(p)
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}
	if !ok {
		t.Errorf("Exists() = false, want true for existing path")
	}
}

func TestOSFS_MkdirAll_CreatesNested(t *testing.T) {
	fs := bundlefs.New()
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := fs.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat nested dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected nested path to be a directory")
	}
}

func TestOSFS_WriteFile_RoundTrip(t *testing.T) {
	fs := bundlefs.New()
	p := filepath.Join(t.TempDir(), "roundtrip.txt")
	if err := fs.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := os.ReadFile(p) //nolint:gosec // test file in t.TempDir
	if err != nil {
		t.Fatalf("reading file back: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want %q", string(data), "hello")
	}
}

func TestOSFS_WriteFile_ExecutablePerm(t *testing.T) {
	fs := bundlefs.New()
	p := filepath.Join(t.TempDir(), "deploy.sh")
	if err := fs.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// On all unix-likes the owner-execute bit must be set.
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("file mode = %v, want executable bit set", info.Mode().Perm())
	}
}

func TestOSFS_WriteFile_MissingParent(t *testing.T) {
	fs := bundlefs.New()
	// Do not create the parent dir; WriteFile must fail loudly — it does
	// not auto-create parents by contract.
	p := filepath.Join(t.TempDir(), "nope", "file.txt")
	if err := fs.WriteFile(p, []byte("x"), 0o600); err == nil {
		t.Errorf("WriteFile() error = nil, want error for missing parent")
	}
}
