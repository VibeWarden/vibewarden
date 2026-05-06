// Package architecture_test — domain purity and config-layer isolation invariants.
//
// This file guards two boundaries:
//
//  1. TestDomainPackages_NoExternalImports — no file under internal/domain/ may
//     import from the adapters, app, cli, ports, plugins, or middleware layers.
//     The single pre-approved exception is internal/domain/site/site.go importing
//     internal/config (ADR-068 boundary exception). Any new exception requires
//     editing the allowedExceptions map and justifying the decision in an ADR.
//
//  2. TestConfigPackage_NoOuterLayerImports — no file under internal/config/ may
//     import from adapters, app, cli, plugins, or middleware. Config is an
//     inward-facing package used by all layers; it must not reach outward.
package architecture_test

import (
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainPackages_NoExternalImports asserts that no non-test Go file under
// internal/domain/ imports from the adapters, app, cli, ports, plugins, or
// middleware layers. The domain layer must have zero external dependencies so
// that use cases can be tested without any infrastructure (hexagonal
// architecture, ADR-064).
//
// The single allowed exception — internal/domain/site/site.go importing
// internal/config — is encoded as a (relPath, importPath) tuple. Adding a
// second exception requires editing this test and justifying the decision.
func TestDomainPackages_NoExternalImports(t *testing.T) {
	const (
		modulePrefix = "github.com/vibewarden/vibewarden/"
		pkgRoot      = "internal/domain"
	)

	// bannedPrefixes are module-relative import prefixes that the domain layer
	// must not reference.
	bannedPrefixes := []string{
		modulePrefix + "internal/adapters/",
		modulePrefix + "internal/app/",
		modulePrefix + "internal/cli/",
		modulePrefix + "internal/ports/",
		modulePrefix + "internal/plugins/",
		modulePrefix + "internal/middleware/",
	}

	// allowedExceptions maps a file's path relative to the pkgRoot to the one
	// import it is permitted to make into a banned prefix.
	// Key:   slash-separated path relative to internal/domain (e.g. "site/site.go")
	// Value: the exact import path that is allowed for that file
	allowedExceptions := map[string]string{
		// ADR-068: site domain depends on config for SiteConfig value objects.
		// This is a pragmatic exception; all other domain files must be clean.
		"site/site.go": modulePrefix + "internal/config",
	}

	moduleRootDir := resolveModuleRoot(t)
	targetDir := filepath.Join(moduleRootDir, filepath.FromSlash(pkgRoot))

	fset := token.NewFileSet()

	err := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(targetDir, path)
		relSlash := filepath.ToSlash(rel)

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parsing %s: %v", path, parseErr)
			return nil
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range bannedPrefixes {
				if !strings.HasPrefix(importPath, banned) {
					continue
				}
				// Check whether this (file, import) pair is an allowed exception.
				if allowed, ok := allowedExceptions[relSlash]; ok && allowed == importPath {
					continue
				}
				t.Errorf("%s/%s imports %q — forbidden by ADR-064 (domain layer must have zero external deps). "+
					"If this exception is intentional, add it to allowedExceptions in this test with an ADR reference.",
					pkgRoot, relSlash, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", pkgRoot, err)
	}
}

// TestConfigPackage_NoOuterLayerImports asserts that no non-test Go file under
// internal/config/ imports from adapters, app, cli, plugins, or middleware.
// Config is consumed by all layers; importing back into them creates cycles and
// breaks the hexagonal architecture's dependency rule.
func TestConfigPackage_NoOuterLayerImports(t *testing.T) {
	const (
		modulePrefix = "github.com/vibewarden/vibewarden/"
		pkgRoot      = "internal/config"
	)

	bannedPrefixes := []string{
		modulePrefix + "internal/adapters/",
		modulePrefix + "internal/app/",
		modulePrefix + "internal/cli/",
		modulePrefix + "internal/plugins/",
		modulePrefix + "internal/middleware/",
	}

	moduleRootDir := resolveModuleRoot(t)
	targetDir := filepath.Join(moduleRootDir, filepath.FromSlash(pkgRoot))

	fset := token.NewFileSet()

	err := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(targetDir, path)

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parsing %s: %v", path, parseErr)
			return nil
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range bannedPrefixes {
				if strings.HasPrefix(importPath, banned) {
					t.Errorf("%s/%s imports %q — forbidden (internal/config must not import outer layers). "+
						"Config is consumed by all layers; importing back into them creates import cycles.",
						pkgRoot, filepath.ToSlash(rel), importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", pkgRoot, err)
	}
}

// resolveModuleRoot returns the on-disk path of the module root by importing
// the always-present internal/ports package and climbing two directories up.
// Defined here for use by tests in this file; the shared helper in
// ports_purity_test.go is unexported and in the same package so it cannot be
// called — each file that needs it must inline it or use build.Default directly.
func resolveModuleRoot(t *testing.T) string {
	t.Helper()

	sentinel, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", build.FindOnly)
	if err != nil {
		t.Fatalf("locating module root via internal/ports: %v", err)
	}
	// sentinel.Dir is <moduleRoot>/internal/ports — go up two levels.
	return filepath.Dir(filepath.Dir(sentinel.Dir))
}
