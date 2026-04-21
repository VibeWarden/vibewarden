package ports

import (
	"context"
	"io/fs"
)

// BundleFS is the filesystem port used by the bundle service to write and
// inspect artifacts. Implementations back onto the real filesystem in
// production and onto an in-memory map in tests. Paths passed to these
// methods are absolute.
//
// The port exists so the bundle use case stays testable without any real
// disk I/O — unit tests drive a fake BundleFS, integration tests drive the
// osfs adapter.
type BundleFS interface {
	// Exists reports whether a file or directory exists at path. It returns
	// (false, nil) when the path is absent and (false, err) for unexpected
	// stat failures (permission denied, etc).
	Exists(path string) (bool, error)

	// ReadFile returns the contents of the named file. Implementations
	// must return an error satisfying errors.Is(err, fs.ErrNotExist) when
	// the file does not exist; callers rely on this sentinel to
	// distinguish a missing file from a corrupt one.
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to path with the given file mode, creating or
	// truncating the file as necessary. Parent directories must exist — use
	// MkdirAll first.
	WriteFile(path string, data []byte, perm fs.FileMode) error

	// MkdirAll creates path and any necessary parent directories with the
	// given directory mode. It is a no-op when path already exists as a
	// directory.
	MkdirAll(path string, perm fs.FileMode) error
}

// ImageSaver saves a local Docker image to a tar archive. This is the same
// shape as ports.ImageExporter but scoped to the bundle use case so the
// bundle service does not pick up the rsync-to-remote semantics baked into
// the deploy Service. Implementations shell out to "docker save -o".
//
// The interface is deliberately narrow so an in-memory fake can satisfy it
// in tests without any docker daemon.
type ImageSaver interface {
	// Save writes the named Docker image to destPath in the OCI tarball
	// format produced by "docker save". destPath is an absolute path on the
	// local filesystem. The image must exist in the local daemon — a
	// missing image returns a wrapped error.
	Save(ctx context.Context, imageTag, destPath string) error
}
