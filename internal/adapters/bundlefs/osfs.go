// Package bundlefs provides a BundleFS implementation backed by the OS
// filesystem. It is the default BundleFS wired into "vibew bundle".
package bundlefs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// OSFS is a BundleFS implementation that reads and writes the real
// filesystem. It is safe to share across goroutines; all operations are
// stateless.
type OSFS struct{}

// New returns a new OSFS.
func New() *OSFS {
	return &OSFS{}
}

// Exists reports whether a file or directory exists at path. It returns
// (false, nil) when the path does not exist and (false, err) when stat
// fails for any other reason (permission denied, I/O error, etc).
func (OSFS) Exists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	return true, nil
}

// ReadFile returns the contents of the file at path. It returns an error
// satisfying errors.Is(err, fs.ErrNotExist) when the file is absent.
func (OSFS) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is controlled by the bundle service
	if err != nil {
		// Do not wrap: callers rely on errors.Is(err, fs.ErrNotExist).
		return nil, err
	}
	return data, nil
}

// WriteFile writes data to path with the given file mode, creating or
// truncating the file as necessary. Parent directories must already exist —
// call MkdirAll first.
func (OSFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	//nolint:gosec // path is controlled by the bundle service; perm is caller-supplied per artifact
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// MkdirAll creates path and any necessary parent directories with the given
// directory mode. It is a no-op when path already exists as a directory.
func (OSFS) MkdirAll(path string, perm fs.FileMode) error {
	// Normalise via Clean so callers that pass trailing slashes still get a
	// tidy error message on failure.
	if err := os.MkdirAll(filepath.Clean(path), perm); err != nil {
		return fmt.Errorf("creating directory %s: %w", path, err)
	}
	return nil
}
