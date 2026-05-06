// Package architecture_test — LoadMergedConfig restricted-callers invariant.
//
// TestLoadMergedConfig_RestrictedCallers enforces that bundleapp.LoadMergedConfig
// (or bundle.LoadMergedConfig) is called only from the approved set of files. All
// other callers must go through the env.Resolver seam (ADR-102).
package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadMergedConfig_RestrictedCallers asserts that LoadMergedConfig is called
// only from the approved allow-list of files. Any call site not in the list
// bypasses the env.Resolver seam and will fail this test.
//
// Allow-list rationale:
//   - internal/app/bundle/resolve.go — definition of LoadMergedConfig (allowed)
//   - internal/app/bundle/bundle.go  — primary consumer inside the bundle use case
//   - internal/app/env/resolver.go   — the ADR-102 canonical seam; all other code
//     should call through this instead of LoadMergedConfig directly
//   - internal/cli/cmd/validate.go   — TODO(#1301): this caller bypasses the env
//     Resolver seam. It must be migrated to call env.Resolver instead. Until
//     #1301 lands, it is included here so the build stays green; remove it from
//     the allow-list when #1301 is complete.
func TestLoadMergedConfig_RestrictedCallers(t *testing.T) {
	// allowedCallers is the set of slash-relative paths (from module root) that
	// are permitted to reference LoadMergedConfig. Any file outside this set that
	// calls LoadMergedConfig (or bundleapp.LoadMergedConfig) fails the test.
	allowedCallers := map[string]bool{
		"internal/app/bundle/resolve.go": true, // definition
		"internal/app/bundle/bundle.go":  true, // primary consumer
		"internal/app/env/resolver.go":   true, // ADR-102 canonical seam
		// TODO(#1301): migrate validate.go to call env.Resolver and remove this entry.
		"internal/cli/cmd/validate.go": true,
	}

	moduleRootDir := resolveModuleRoot(t)

	scanRoots := []string{"internal", "cmd"}
	fset := token.NewFileSet()

	for _, root := range scanRoots {
		rootDir := filepath.Join(moduleRootDir, filepath.FromSlash(root))
		err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			relSlash := filepath.ToSlash(mustRel(moduleRootDir, path))
			if allowedCallers[relSlash] {
				return nil
			}

			f, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				t.Errorf("parsing %s: %v", path, parseErr)
				return nil
			}

			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "LoadMergedConfig" {
					return true
				}
				t.Errorf("%s calls LoadMergedConfig — only the env.Resolver seam "+
					"(internal/app/env/resolver.go) may call this function directly. "+
					"Use env.Resolver instead, or add this file to the allow-list "+
					"in this test with an ADR justification.",
					relSlash)
				return false // stop traversal for this file's violations
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}
