package quality_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemovedArtifacts_NoForbiddenReferences loads .github/removed-artifacts.txt
// and walks the repository, failing if any removed-artifact name appears in a
// text file outside the path allowlist.
//
// Add entries to .github/removed-artifacts.txt when deprecating a named
// artifact (file, command, etc.). Add entries to the path allowlist below when
// a file legitimately must contain a removed-artifact name (e.g. a guard test
// that asserts the artifact is absent from generated output).
//
// This guard runs automatically under `go test ./...` (and therefore under
// `make check` and CI) — no separate workflow needed. See issue #1201.
func TestRemovedArtifacts_NoForbiddenReferences(t *testing.T) {
	// Resolve repo root: this file is at internal/quality/; two levels up.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	artifacts := loadRemovedArtifacts(t, repoRoot)
	if len(artifacts) == 0 {
		// Empty allowlist — nothing to check. Trivially pass.
		return
	}

	// pathAllowlist contains path substrings that are excluded from the guard
	// walk. A file is skipped when its absolute path contains any of these
	// substrings. Entries fall into four categories:
	//
	//  1. Infrastructure: .git/, .claude/worktrees/ — never source files.
	//  2. Historical archives: CHANGELOG.md, decisions/ — intentionally
	//     preserve removed-artifact names for provenance.
	//  3. Guard infrastructure: the allowlist file itself and this test file —
	//     both must contain the names they guard against.
	//  4. Negative-assertion guards: test files that contain a removed-artifact
	//     name specifically to assert it does NOT appear in generated output.
	//     These are quality guards, not stale references.
	//  5. Explanatory docs: files that mention a removed artifact to explain
	//     why it was removed (historical rationale, not a live instruction).
	pathAllowlist := []string{
		// Infrastructure.
		string(filepath.Separator) + ".git" + string(filepath.Separator),
		".claude" + string(filepath.Separator) + "worktrees" + string(filepath.Separator),
		// Historical archives.
		"CHANGELOG.md",
		string(filepath.Separator) + "decisions" + string(filepath.Separator),
		"decisions" + string(filepath.Separator),
		// Guard infrastructure — these files must contain the names.
		".github" + string(filepath.Separator) + "removed-artifacts.txt",
		"internal" + string(filepath.Separator) + "quality" + string(filepath.Separator) + "removed_artifacts_test.go",
		// testdata/ under internal/quality/ holds positive-violation fixtures
		// used by TestRemovedArtifacts_FlagsViolations below.
		"internal" + string(filepath.Separator) + "quality" + string(filepath.Separator) + "testdata" + string(filepath.Separator),
		// Negative-assertion guards: test files that assert a removed artifact
		// does NOT appear in generated output. The presence of the name here is
		// intentional — it is the token being asserted against.
		"bundle_extras_test.go",
		"bundle_test.go",
		"prompt_template_test.go",
		"golden_test.go",
		"osfs_test.go",
		"release_artifact_test.go",
		"wrapper_script_test.go",
		// Explanatory docs: historical context for why an artifact was removed.
		"docs" + string(filepath.Separator) + "agent-kickoff.md",
		"llms-full.txt",
	}

	// scannableExt is the set of file extensions the guard scans. Everything
	// else (binaries, images, archives) is skipped. Extensionless files are
	// included so that Makefile, LICENSE, and Dockerfile are covered.
	scannableExt := map[string]bool{
		".go":   true,
		".md":   true,
		".yaml": true,
		".yml":  true,
		".txt":  true,
		".tmpl": true,
		".toml": true,
		".json": true,
		"":      true, // Makefile, LICENSE, Dockerfile, etc.
	}

	// skipDir is the set of directory names to skip entirely.
	skipDir := map[string]bool{
		".git":         true,
		"vendor":       true,
		"node_modules": true,
		"dist":         true,
		"bin":          true,
		"build":        true,
	}

	err = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDir[info.Name()] {
				return filepath.SkipDir
			}
			// Skip .claude/worktrees/ transient agent worktrees.
			if strings.Contains(path, filepath.Join(".claude", "worktrees")) {
				return filepath.SkipDir
			}
			return nil
		}

		// Extension filter.
		ext := strings.ToLower(filepath.Ext(path))
		if !scannableExt[ext] {
			return nil
		}

		// Path allowlist check.
		for _, allowed := range pathAllowlist {
			if strings.Contains(path, allowed) {
				return nil
			}
		}

		data, err := os.ReadFile(path) //nolint:gosec // guard walks known repo tree
		if err != nil {
			// Non-fatal: log and continue so a single unreadable file does not
			// abort the entire walk.
			t.Logf("warning: cannot read %s: %v", path, err)
			return nil
		}

		// Skip binary files: null bytes are a reliable binary sentinel. This
		// protects against extensionless compiled binaries (e.g. the root
		// `vibew` Mach-O binary in development checkouts) being treated as
		// text and producing spurious matches inside binary data.
		if bytes.Contains(data, []byte{0}) {
			return nil
		}

		rel, _ := filepath.Rel(repoRoot, path)
		lines := strings.Split(string(data), "\n")
		for _, artifact := range artifacts {
			for i, line := range lines {
				col := strings.Index(line, artifact)
				if col < 0 {
					continue
				}
				t.Errorf(
					"removed artifact %q found at %s:%d:%d — "+
						"allowlist via .github/removed-artifacts.txt or "+
						"move the reference into CHANGELOG.md / decisions/",
					artifact, rel, i+1, col+1,
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
}

// TestRemovedArtifacts_FlagsViolations is the positive-violation subtest.
// It points the artifact scanner at internal/quality/testdata/ — a directory
// that contains violation.txt with a known removed-artifact name — and asserts
// the guard correctly detects the violation.
//
// The testdata/ directory is excluded from TestRemovedArtifacts_NoForbiddenReferences
// via the path allowlist so the fixture does not trigger a false positive in the
// production walk.
func TestRemovedArtifacts_FlagsViolations(t *testing.T) {
	// Resolve testdata/ relative to this file: internal/quality/testdata/.
	testdataDir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("resolving testdata dir: %v", err)
	}

	// Load the real allowlist so the test uses the same artifact names.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	artifacts := loadRemovedArtifacts(t, repoRoot)
	if len(artifacts) == 0 {
		t.Skip("removed-artifacts.txt is empty; no violation to detect")
	}

	// Walk testdata/ and collect all artifact occurrences.
	type hit struct {
		file     string
		line     int
		artifact string
	}
	var hits []hit

	err = filepath.Walk(testdataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // test walks known testdata dir
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for _, artifact := range artifacts {
			for i, line := range lines {
				if strings.Contains(line, artifact) {
					hits = append(hits, hit{file: path, line: i + 1, artifact: artifact})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking testdata: %v", err)
	}

	if len(hits) == 0 {
		t.Errorf("positive-violation test found no removed-artifact references in %s — "+
			"add at least one removed-artifact name to testdata/violation.txt "+
			"so the guard is proven to catch violations", testdataDir)
	} else {
		t.Logf("positive-violation test correctly found %d hit(s) in testdata/:", len(hits))
		for _, h := range hits {
			t.Logf("  %s:%d contains %q", h.file, h.line, h.artifact)
		}
	}
}

// loadRemovedArtifacts reads .github/removed-artifacts.txt from the repo root,
// strips blank lines and # comments, and returns the remaining tokens. It calls
// t.Fatal if the file is missing — the guard cannot run without the registry.
func loadRemovedArtifacts(t *testing.T, repoRoot string) []string {
	t.Helper()
	registryPath := filepath.Join(repoRoot, ".github", "removed-artifacts.txt")
	f, err := os.Open(registryPath) //nolint:gosec // path is constructed from known repo layout
	if err != nil {
		t.Fatalf("removed-artifacts registry missing at %s — restore .github/removed-artifacts.txt: %v",
			registryPath, err)
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is irrelevant

	var artifacts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		artifacts = append(artifacts, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading removed-artifacts registry: %v", err)
	}
	return artifacts
}
