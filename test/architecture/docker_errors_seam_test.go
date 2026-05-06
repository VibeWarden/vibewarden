// Package architecture_test — docker error detection seam invariant.
//
// TestDockerErrorDetection_OnlyInClassifyDockerError enforces that
// ClassifyDockerError in internal/adapters/ops/docker_errors.go is the single
// canonical place where docker-specific error strings are matched. No other
// non-test file may contain strings.Contains calls whose string literal contains
// signatures that indicate docker binary absence or daemon unavailability.
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

// TestDockerErrorDetection_OnlyInClassifyDockerError asserts that docker error
// detection string literals appear only in the canonical seam
// (internal/adapters/ops/docker_errors.go). Any other non-test Go file that
// contains a strings.Contains call whose string argument matches a docker-error
// signature will fail this test.
//
// Background: prior to this invariant, image_inspect.go:145 contained an inline
// check for "exec: \"docker\": executable file not found in $PATH". That check
// was migrated into ClassifyDockerError as part of issue #1303. This test
// prevents the pattern from re-appearing elsewhere.
//
// Exempt files:
//   - internal/adapters/ops/docker_errors.go (the canonical seam — allowed)
//   - internal/adapters/ops/docker_errors_test.go (test coverage — allowed)
func TestDockerErrorDetection_OnlyInClassifyDockerError(t *testing.T) {
	moduleRootDir := resolveModuleRoot(t)

	// dockerErrSignatures are substrings that, when found inside a
	// strings.Contains string-literal argument, indicate docker error detection
	// logic that belongs exclusively in ClassifyDockerError.
	dockerErrSignatures := []string{
		"permission denied",
		"daemon",
		"cannot connect to the docker",
		"docker: executable file not found",
		`exec: "docker"`,
		"executable file not found",
	}

	// exemptFiles are slash-relative paths from the module root that are
	// permitted to contain these literals (the canonical seam and its test).
	exemptFiles := map[string]bool{
		"internal/adapters/ops/docker_errors.go":      true,
		"internal/adapters/ops/docker_errors_test.go": true,
	}

	// scanRoots lists the directory subtrees to walk.
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
			if exemptFiles[relSlash] {
				return nil
			}

			f, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				t.Errorf("parsing %s: %v", path, parseErr)
				return nil
			}

			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				// Match calls of the form strings.Contains(x, <literal>)
				// or strings.ContainsAny / strings.HasPrefix — we only care
				// about strings.Contains here since that's the pattern used
				// for docker error detection.
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if pkgIdent.Name != "strings" || sel.Sel.Name != "Contains" {
					return true
				}
				if len(call.Args) < 2 {
					return true
				}

				lit, ok := call.Args[1].(*ast.BasicLit)
				if !ok {
					return true
				}
				// Strip surrounding quotes from the string literal.
				litVal := strings.ToLower(strings.Trim(lit.Value, `"`))

				for _, sig := range dockerErrSignatures {
					if strings.Contains(litVal, strings.ToLower(sig)) {
						t.Errorf("%s: strings.Contains call with docker error signature %q found outside the canonical seam "+
							"(internal/adapters/ops/docker_errors.go). "+
							"Migrate this check into ClassifyDockerError instead.",
							relSlash, sig)
						// Report only once per call site.
						break
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}

// mustRel is filepath.Rel that panics on error (both paths are known-good
// module-root-relative paths produced by WalkDir in this package).
func mustRel(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		panic(err)
	}
	return rel
}
