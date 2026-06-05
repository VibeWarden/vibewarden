// Package bundle provides the application service that produces the
// `vibew bundle` deployment artifact.
//
// A Service orchestrates the pipeline that writes docker-compose.yml,
// the merged vibewarden.yaml, sample.env, .env, README.md, MANIFEST.md,
// and (unless skipped) image.tar under a caller-supplied output
// directory. The pipeline is purely local: no SSH connection is opened
// and no remote state is touched — the user owns the transport. See
// ADR-085 (vibew bundle contract) and ADR-086 (sunset vibew deploy).
package bundle

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service produces the `vibew bundle` deployment artifact.
type Service struct {
	generator ports.ConfigGenerator

	// version is the CLI build version string (e.g. "0.20.0" for a release,
	// "dev" for a source build). Used by renderSidecarCompose to compute the
	// sidecar image reference via config.SidecarImageRef. Empty string falls
	// back to the dev/latest behaviour.
	version string

	// bundleFS is the filesystem adapter used by the bundle extras pipeline
	// (sample.env, .env, README.md, MANIFEST.md). When nil, the extras pipeline
	// is a no-op.
	bundleFS ports.BundleFS

	// imageSaver saves the app image to image.tar during vibew bundle. When
	// nil, the extras pipeline treats --skip-image as always-on for the
	// invocation.
	imageSaver ports.ImageSaver

	// imageInspector inspects the target image via `docker image inspect`
	// before any bundle files are written. When nil, the health block is
	// skipped (backward-compatible path for existing tests).
	imageInspector ports.ImageInspector

	// stalenessWalker computes the most-recent source file mtime for the
	// freshness verdict. When nil, freshness reporting is disabled.
	stalenessWalker StalenessWalker
}

// NewService creates a Service.
//
// generator is used to produce the generated/ files referenced by the
// bundle. executor is accepted for backwards compatibility with existing
// call sites that pass a nil value; the bundle pipeline does not use it.
func NewService(_ ports.RemoteExecutor, generator ports.ConfigGenerator) *Service {
	return &Service{generator: generator}
}

// WithVersion sets the CLI build version used to compute the sidecar image
// reference in the rendered sidecar docker-compose.yml. For release builds
// (e.g. "0.20.0") the sidecar image is pinned to that version tag; for
// dev/source builds (e.g. "dev") the image falls back to :latest with
// pull_policy: always. When not called, the dev/fallback path is used.
func (s *Service) WithVersion(version string) *Service {
	s.version = version
	return s
}

// WithBundleFS sets the filesystem adapter used by the bundle extras
// pipeline and returns the Service for chaining. Production callers pass a
// bundlefs.OSFS; tests pass an in-memory fake.
func (s *Service) WithBundleFS(bfs ports.BundleFS) *Service {
	s.bundleFS = bfs
	return s
}

// WithImageSaver sets the ImageSaver used by the bundle extras pipeline and
// returns the Service for chaining. When not set, image.tar is never
// produced (equivalent to --skip-image).
func (s *Service) WithImageSaver(saver ports.ImageSaver) *Service {
	s.imageSaver = saver
	return s
}

// WithImageInspector sets the ImageInspector used to produce the image health
// block before any bundle files are written. When not set, the health block is
// skipped — this is the nil-safe path used by existing tests that predate
// ADR-089.
func (s *Service) WithImageInspector(inspector ports.ImageInspector) *Service {
	s.imageInspector = inspector
	return s
}

// WithStalenessWalker sets the StalenessWalker used to compute the freshness
// verdict in the image health block. When not set, freshness reporting is
// disabled.
func (s *Service) WithStalenessWalker(walker StalenessWalker) *Service {
	s.stalenessWalker = walker
	return s
}

// WriteInputDigest computes the SHA-256 content digest for projectRoot and
// writes it to <projectRoot>/.vibewarden/.input-digest (schema v2). It also
// writes <projectRoot>/.vibewarden/.gitignore containing "*\n" so the whole
// .vibewarden/ directory is excluded from git without touching the user's own
// .gitignore.
//
// This must be called only after a successful Bundle() invocation. A write
// failure is logged at warn level and does not return an error — the next run
// will treat the missing digest as a first-run baseline (FRESH), which is the
// correct degraded behaviour.
func (s *Service) WriteInputDigest(projectRoot string) {
	if projectRoot == "" {
		return
	}
	// Write the per-directory .gitignore first (idempotent) so that future git
	// operations exclude .vibewarden/ without any modification to the user's
	// own .gitignore.
	if err := ensureVibewardenGitignoreFile(projectRoot); err != nil {
		slog.Warn("input-digest: cannot write .vibewarden/.gitignore", "err", err)
		// Continue — digest write is still attempted.
	}
	d, err := computeInputDigest(projectRoot)
	if err != nil {
		slog.Debug("input-digest: compute failed, skipping write", "err", err)
		return
	}
	writeInputDigest(projectRoot, d)
}

// ProjectNameFromConfig derives a project name from the config file path.
// It returns the base name of the directory containing the config file,
// sanitised so spaces and dots become dashes.
//
// When configPath is empty the current working directory name is used. When
// configPath is relative it is resolved to an absolute path first so that
// filepath.Dir returns the real directory rather than ".".
func ProjectNameFromConfig(configPath string) string {
	if configPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "vibewarden"
		}
		configPath = filepath.Join(wd, "vibewarden.yaml")
	}

	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "vibewarden"
	}

	dir := filepath.Dir(abs)
	name := filepath.Base(dir)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, ".", "-")
	if name == "" || name == "." {
		return "vibewarden"
	}
	return name
}
