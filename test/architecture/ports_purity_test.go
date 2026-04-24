// Package architecture_test holds repo-wide architectural-invariant tests.
//
// This file guards the hexagonal-architecture invariants locked in ADR-064 and
// ADR-067. It runs on every `make check` invocation (go test -race ./...) with
// no build tags required — the ~1.6 s runtime and zero external dependencies
// (stdlib only) make gating unnecessary.
//
// Invariants asserted:
//
//  1. internal/ports/ imports only stdlib and internal/domain/* (ADR-064).
//  2. internal/app/**/*.go (non-test) imports no internal/adapters/* path (ADR-067).
//  3. internal/mcp/**/*.go (non-test) imports no internal/adapters/* or
//     internal/app/* path (ADR-067 Decision 2).
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

func TestPortsPackage_OnlyImportsStdlibAndDomain(t *testing.T) {
	pkg, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", 0)
	if err != nil {
		t.Fatalf("importing internal/ports: %v", err)
	}

	const (
		moduleRoot      = "github.com/vibewarden/vibewarden/"
		allowedInternal = moduleRoot + "internal/domain/"
	)

	banned := []string{
		moduleRoot + "internal/config",
		moduleRoot + "internal/adapters",
		moduleRoot + "internal/app",
	}

	// The stdlib is always allowed (Go treats stdlib imports as import paths
	// without a dot in the first element). Module-relative imports must be
	// under internal/domain/.
	for _, imp := range pkg.Imports {
		if !strings.HasPrefix(imp, moduleRoot) {
			continue // stdlib or external module — not checked here
		}
		for _, bad := range banned {
			if strings.HasPrefix(imp, bad) {
				t.Errorf("internal/ports imports %q — forbidden by ADR-064", imp)
			}
		}
		if !strings.HasPrefix(imp, allowedInternal) && imp != moduleRoot+"internal/ports" {
			t.Errorf("internal/ports imports %q — only stdlib and %s*** are allowed",
				imp, allowedInternal)
		}
	}
}

// TestAppPackages_NoAdapterImports asserts that no non-test Go file under
// internal/app/ imports any path matching internal/adapters/. This encodes
// the ADR-067 invariant: the app layer must remain adapter-free so that use
// cases can be tested without infrastructure.
func TestAppPackages_NoAdapterImports(t *testing.T) {
	const (
		moduleRoot  = "github.com/vibewarden/vibewarden/"
		bannedInfix = moduleRoot + "internal/adapters/"
		pkgRoot     = "internal/app"
	)

	assertNoForbiddenImports(t, pkgRoot, []string{bannedInfix})
}

// TestMCPPackage_NoAdapterOrAppImports asserts that no non-test Go file under
// internal/mcp/ imports any path matching internal/adapters/ or internal/app/.
// This encodes ADR-067 Decision 2: the MCP layer defines its own local
// interface types and must not reach into the app or adapter layers.
func TestMCPPackage_NoAdapterOrAppImports(t *testing.T) {
	const (
		moduleRoot = "github.com/vibewarden/vibewarden/"
		pkgRoot    = "internal/mcp"
	)

	banned := []string{
		moduleRoot + "internal/adapters/",
		moduleRoot + "internal/app/",
	}

	assertNoForbiddenImports(t, pkgRoot, banned)
}

// TestCaddyAdapter_NoPeerAdapterImports asserts that no non-test Go file under
// internal/adapters/caddy/ imports any peer adapter package (i.e. any path
// matching internal/adapters/ other than internal/adapters/caddy itself).
//
// This guards the ADR-092 invariant: Caddy handler modules must receive their
// dependencies via RuntimeServices, not by directly constructing sibling adapters.
func TestCaddyAdapter_NoPeerAdapterImports(t *testing.T) {
	const (
		moduleRoot  = "github.com/vibewarden/vibewarden/"
		pkgRoot     = "internal/adapters/caddy"
		caddyPkg    = moduleRoot + "internal/adapters/caddy"
		adapterBase = moduleRoot + "internal/adapters/"
	)

	sentinel, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", build.FindOnly)
	if err != nil {
		t.Fatalf("locating module root via internal/ports: %v", err)
	}
	moduleRootDir := filepath.Dir(filepath.Dir(sentinel.Dir))
	targetDir := filepath.Join(moduleRootDir, filepath.FromSlash(pkgRoot))

	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parsing %s: %v", path, parseErr)
			return nil
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, adapterBase) && !strings.HasPrefix(importPath, caddyPkg) {
				rel, _ := filepath.Rel(targetDir, path)
				t.Errorf("%s/%s imports peer adapter %q — forbidden by ADR-092", pkgRoot, rel, importPath)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", pkgRoot, walkErr)
	}
}

// assertNoForbiddenImports walks pkgRoot (relative to the repo root resolved
// via go/build), parses every non-test .go file, and fails the test for each
// import that has any of the forbidden prefixes.
//
// pkgRoot is a directory path relative to the module root, not necessarily a
// Go package itself (e.g. "internal/app" contains sub-packages only).
func assertNoForbiddenImports(t *testing.T, pkgRoot string, forbidden []string) {
	t.Helper()

	// Resolve the module root's on-disk path by importing a known package
	// (internal/ports always exists) and climbing two directories up.
	sentinel, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", build.FindOnly)
	if err != nil {
		t.Fatalf("locating module root via internal/ports: %v", err)
	}
	// sentinel.Dir is <moduleRoot>/internal/ports — go up two levels.
	moduleRootDir := filepath.Dir(filepath.Dir(sentinel.Dir))
	targetDir := filepath.Join(moduleRootDir, filepath.FromSlash(pkgRoot))

	fset := token.NewFileSet()

	err = filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parsing %s: %v", path, parseErr)
			return nil
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if strings.HasPrefix(importPath, bad) {
					rel, _ := filepath.Rel(targetDir, path)
					t.Errorf("%s/%s imports %q — forbidden by ADR-067", pkgRoot, rel, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", pkgRoot, err)
	}
}
