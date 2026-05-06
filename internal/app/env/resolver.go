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
	// project root. When the file already exists we call filepath.EvalSymlinks
	// to dereference any symlink (e.g. vibewarden.prod.yaml → /etc/passwd).
	// Only when the file exists: EvalSymlinks errors on missing paths, and a
	// missing file is safe — the read below will fail with "file not found".
	// We also resolve the root itself so that platform symlinks (e.g. macOS
	// /var → /private/var inside t.TempDir()) do not cause false positives.
	// When the file is absent, containment is computed against the real root
	// using a path re-joined from the resolved root (never the raw name).
	realRoot := root
	if rr, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		realRoot = rr
	}
	// containmentTarget is what we actually check for containment. When the
	// file is missing we re-join from realRoot so both sides of Rel share the
	// same symlink resolution (avoids /var vs /private/var mismatches on macOS).
	containmentTarget := filepath.Join(realRoot, "vibewarden."+name+".yaml")
	if _, statErr := os.Stat(overridePath); statErr == nil {
		resolved, evalErr := filepath.EvalSymlinks(overridePath)
		if evalErr != nil {
			return Resolved{}, fmt.Errorf("resolving override path %s: %w", overridePath, evalErr)
		}
		containmentTarget = resolved
	}
	rel, relErr := filepath.Rel(realRoot, containmentTarget)
	if relErr != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
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

// ResolvePath applies the same allowlist and EvalSymlinks containment checks
// as Resolve but returns only the override path without loading or validating
// the merged configuration. This is used by callers (e.g. vibew bundle, vibew
// validate) that need the validated path so they can load the config with their
// own stricter loader (e.g. config.LoadStrict) rather than the lenient
// bundleapp.LoadMergedConfig used inside Resolve.
//
// When name is empty or the override file is absent, ResolvePath returns ("",
// nil) — absent override is not an error at this level; callers decide whether
// to treat it as "base-only" mode.
//
// Returns ErrInvalidEnvName when name fails the allowlist or the resolved path
// escapes ProjectRoot (path-traversal / symlink escape). Returns a non-nil
// error for I/O failures (stat, EvalSymlinks) other than file-not-found.
func (r *FileResolver) ResolvePath(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if err := validateEnvName(name); err != nil {
		return "", err
	}

	root, err := r.projectRoot()
	if err != nil {
		return "", fmt.Errorf("resolving project root: %w", err)
	}

	overridePath := filepath.Join(root, "vibewarden."+name+".yaml")

	// EvalSymlinks containment check — mirrors Resolve's defence-in-depth.
	realRoot := root
	if rr, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		realRoot = rr
	}
	containmentTarget := filepath.Join(realRoot, "vibewarden."+name+".yaml")
	if _, statErr := os.Stat(overridePath); statErr == nil {
		resolved, evalErr := filepath.EvalSymlinks(overridePath)
		if evalErr != nil {
			return "", fmt.Errorf("resolving override path %s: %w", overridePath, evalErr)
		}
		containmentTarget = resolved
	}
	rel, relErr := filepath.Rel(realRoot, containmentTarget)
	if relErr != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("%w: resolved path escapes project root", ErrInvalidEnvName)
	}

	// Return the path only when the file exists.
	if _, err := os.Stat(overridePath); err != nil {
		if os.IsNotExist(err) {
			return "", nil // absent override — base-only mode
		}
		return "", fmt.Errorf("accessing override config %s: %w", overridePath, err)
	}
	return overridePath, nil
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
