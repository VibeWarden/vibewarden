// Package env provides the canonical env-resolver for VibeWarden CLI verbs
// that accept an --env <name> flag.
//
// The Resolver interface is the single entry point for env-flag handling across
// all CLI commands. Future verbs (vibew status --env prod, vibew validate --env
// prod, etc.) must route through this package rather than rolling their own
// ad-hoc config discovery helpers. See ADR-102 for the architectural rationale.
package env

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	"github.com/vibewarden/vibewarden/internal/config"
)

// envNameRE is the allowlist pattern for env names passed via --env.
// Only alphanumerics, hyphens, and underscores are permitted; no slashes,
// dots, NUL bytes, or other path-significant characters.
var envNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ErrInvalidEnvName is returned when the env name contains characters that
// are not permitted (path-traversal sequences, slashes, dots, NUL bytes, or
// any character outside [a-zA-Z0-9_-]).
var ErrInvalidEnvName = errors.New("invalid env name: must match [a-zA-Z0-9_-]+")

// Sentinel errors returned by Resolver implementations.
var (
	// ErrBaseConfigMissing is returned when vibewarden.yaml cannot be found
	// in the project root.
	ErrBaseConfigMissing = errors.New("base config not found: vibewarden.yaml")

	// ErrOverrideConfigMissing is returned when a non-empty env name was
	// given but the corresponding vibewarden.<name>.yaml file does not exist.
	// Explicit env names must map to existing files — there is no fallback to
	// the base config.
	ErrOverrideConfigMissing = errors.New("override config not found")
)

// Resolver loads the merged config for an optional environment name.
//
// When name is empty, only the base vibewarden.yaml is loaded (via
// config.Load). When name is non-empty, vibewarden.<name>.yaml is located in
// the same project root and deep-merged on top of the base via
// bundleapp.LoadMergedConfig. Missing override files are an error — explicit
// env names map to existing files.
type Resolver interface {
	Resolve(name string) (Resolved, error)
}

// Resolved carries the merged config plus the file paths that produced it.
// Future verbs that adopt --env re-use this shape.
type Resolved struct {
	// Cfg is the merged (or base-only) config ready for use.
	Cfg *config.Config

	// BasePath is the absolute path to vibewarden.yaml.
	BasePath string

	// OverridePath is the absolute path to vibewarden.<name>.yaml.
	// Empty when name == "" (base-only resolve).
	OverridePath string

	// EnvName echoes the name argument; empty for the default (base-only) resolve.
	EnvName string
}

// FileResolver is the default Resolver implementation. It locates config files
// on the local filesystem relative to ProjectRoot.
//
// When ProjectRoot is empty, os.Getwd() is used at Resolve time.
type FileResolver struct {
	// ProjectRoot is the directory that contains vibewarden.yaml.
	// Defaults to the caller's working directory when empty.
	ProjectRoot string
}

// NewFileResolver constructs a FileResolver rooted at projectRoot.
// Pass an empty string to use the caller's working directory.
func NewFileResolver(projectRoot string) *FileResolver {
	return &FileResolver{ProjectRoot: projectRoot}
}

// Resolve loads the config for the given env name.
//
// When name is empty, only vibewarden.yaml is loaded via config.Load.
// When name is non-empty, vibewarden.<name>.yaml must exist in ProjectRoot and
// is deep-merged on top of vibewarden.yaml via bundleapp.LoadMergedConfig.
//
// Returns ErrInvalidEnvName when name contains characters outside [a-zA-Z0-9_-]
// (including path-traversal sequences, slashes, dots, NUL bytes, or a leading dot).
// Returns ErrBaseConfigMissing when vibewarden.yaml cannot be found.
// Returns ErrOverrideConfigMissing when name is non-empty and the override
// file does not exist.
func (r *FileResolver) Resolve(name string) (Resolved, error) {
	if name != "" {
		if err := validateEnvName(name); err != nil {
			return Resolved{}, err
		}
	}

	root, err := r.projectRoot()
	if err != nil {
		return Resolved{}, fmt.Errorf("resolving project root: %w", err)
	}

	basePath := filepath.Join(root, "vibewarden.yaml")
	if _, err := os.Stat(basePath); err != nil {
		if os.IsNotExist(err) {
			return Resolved{}, fmt.Errorf("%w: %s", ErrBaseConfigMissing, basePath)
		}
		return Resolved{}, fmt.Errorf("accessing base config %s: %w", basePath, err)
	}

	if name == "" {
		cfg, err := config.Load(basePath)
		if err != nil {
			return Resolved{}, fmt.Errorf("loading base config: %w", err)
		}
		return Resolved{
			Cfg:          cfg,
			BasePath:     basePath,
			OverridePath: "",
			EnvName:      "",
		}, nil
	}

	overridePath := filepath.Join(root, "vibewarden."+name+".yaml")

	// Defense-in-depth: verify the resolved override path has not escaped the
	// project root. filepath.Rel returns a path starting with ".." when the
	// target is outside the base. This guard catches any edge cases that slip
	// past the name allowlist (e.g. symlinks that the allowlist cannot see).
	rel, relErr := filepath.Rel(root, overridePath)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		return Resolved{}, fmt.Errorf("%w: resolved path escapes project root", ErrInvalidEnvName)
	}

	if _, err := os.Stat(overridePath); err != nil {
		if os.IsNotExist(err) {
			return Resolved{}, fmt.Errorf("%w: %s", ErrOverrideConfigMissing, overridePath)
		}
		return Resolved{}, fmt.Errorf("accessing override config %s: %w", overridePath, err)
	}

	cfg, err := bundleapp.LoadMergedConfig(basePath, overridePath)
	if err != nil {
		return Resolved{}, fmt.Errorf("loading merged config for env %q: %w", name, err)
	}

	return Resolved{
		Cfg:          cfg,
		BasePath:     basePath,
		OverridePath: overridePath,
		EnvName:      name,
	}, nil
}

// validateEnvName returns ErrInvalidEnvName when name is empty, contains NUL
// bytes, or contains any character outside the allowlist [a-zA-Z0-9_-].
// The env name is intended to be a single identifier token, not a path
// component.
func validateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidEnvName)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: name contains NUL byte", ErrInvalidEnvName)
	}
	if !envNameRE.MatchString(name) {
		return fmt.Errorf("%w: got %q", ErrInvalidEnvName, name)
	}
	return nil
}

// projectRoot returns the effective project root, resolving empty to os.Getwd.
func (r *FileResolver) projectRoot() (string, error) {
	if r.ProjectRoot != "" {
		return r.ProjectRoot, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	return cwd, nil
}

// Compile-time assertion that FileResolver satisfies Resolver.
var _ Resolver = (*FileResolver)(nil)
