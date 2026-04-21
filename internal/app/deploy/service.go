// Package deploy provides the application service that produces the
// `vibew bundle` deployment artifact.
//
// The package name is a historical artefact from the removed `vibew deploy`
// command and is renamed to `bundle` in a follow-up commit on this branch
// (ADR-086).
package deploy

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service produces the `vibew bundle` deployment artifact.
type Service struct {
	generator ports.ConfigGenerator

	// bundleFS is the filesystem adapter used by the bundle extras pipeline
	// (sample.env, .env, deploy.sh, README.md). When nil, the extras pipeline
	// is a no-op.
	bundleFS ports.BundleFS

	// imageSaver saves the app image to image.tar during vibew bundle. When
	// nil, the extras pipeline treats --skip-image as always-on for the
	// invocation.
	imageSaver ports.ImageSaver
}

// NewService creates a Service.
//
// generator is used to produce the generated/ files referenced by the
// bundle. executor is accepted for backwards compatibility with existing
// call sites that pass a nil value; the bundle pipeline does not use it.
func NewService(_ ports.RemoteExecutor, generator ports.ConfigGenerator) *Service {
	return &Service{generator: generator}
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
