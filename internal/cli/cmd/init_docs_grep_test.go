package cmd_test

// TestInitDocs_NoLyingScaffoldClaims is a regression grep guard for #1202.
//
// It walks docs/, examples/, README.md, llms.txt, and llms-full.txt and fails
// on any phrase that falsely implies vibew init scaffolds application source
// code (Dockerfiles, main.go, starter code, etc.).
//
// Allowlist: CHANGELOG.md and decisions/ are excluded because they may
// legitimately contain historical references to removed behaviour.
// The test file itself is excluded because it must contain the patterns to
// assert against them.
//
// Kept separate from the removed-artifact guard added in #1201
// (internal/quality/removed_artifacts_test.go + .github/removed-artifacts.txt)
// by design — see issue #1201 architect comment. That guard targets removed
// artifact *names* (literal tokens, versioned allowlist). This guard targets
// false *claims about behavior* (regex phrases). Different category, different
// match semantics, different allowlist axis. Re-evaluate unification when a
// third guard appears.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forbiddenScaffoldClaims lists regex patterns that falsely imply vibew init
// scaffolds app source code. All matches are case-insensitive.
var forbiddenScaffoldClaims = []*regexp.Regexp{
	regexp.MustCompile(`(?i)cmd/[^/\s]+/main\.go`),
	regexp.MustCompile(`(?i)init\s+scaffolds\s+.*main\.go`),
	regexp.MustCompile(`(?i)init\s+creates\s+.*main\.go`),
	regexp.MustCompile(`(?i)init\s+generates\s+.*main\.go`),
	regexp.MustCompile(`(?i)scaffolds\s+(?:both\s+)?the\s+app`),
	regexp.MustCompile(`(?i)it\s+scaffolds\s+both\s+the\s+app`),
	regexp.MustCompile(`(?i)starter\s+(?:main|app|code)`),
	regexp.MustCompile(`(?i)Edit\s+cmd/[^/\s]+/main\.go`),
}

// scaffoldClaimAllowlist lists path substrings that are excluded from the
// grep walk (historical references in changelogs, ADRs, and this file itself).
var scaffoldClaimAllowlist = []string{
	"CHANGELOG.md",
	string(filepath.Separator) + "decisions" + string(filepath.Separator),
	"decisions/",
	".git" + string(filepath.Separator),
	".claude" + string(filepath.Separator) + "worktrees" + string(filepath.Separator),
	"init_docs_grep_test.go",
}

func TestInitDocs_NoLyingScaffoldClaims(t *testing.T) {
	// Resolve the repo root: this file is at internal/cli/cmd/; three levels up.
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	// Targets to walk: docs/, examples/, README.md, llms.txt, llms-full.txt.
	targets := []string{
		filepath.Join(repoRoot, "docs"),
		filepath.Join(repoRoot, "examples"),
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(repoRoot, "llms.txt"),
		filepath.Join(repoRoot, "llms-full.txt"),
	}

	for _, target := range targets {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			// Target doesn't exist in this checkout; skip silently.
			continue
		}

		if err := filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			// Check allowlist.
			for _, allowed := range scaffoldClaimAllowlist {
				if strings.Contains(path, allowed) {
					return nil
				}
			}

			// Only scan text-like files.
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".md", ".txt", ".yaml", ".yml", ".go", ".json", ".toml", "":
				// proceed
			default:
				return nil
			}

			data, err := os.ReadFile(path) //nolint:gosec // test walks known directories
			if err != nil {
				return err
			}

			content := string(data)
			rel, _ := filepath.Rel(repoRoot, path)

			for _, pat := range forbiddenScaffoldClaims {
				if pat.MatchString(content) {
					// Find the matching lines for a useful error message.
					for i, line := range strings.Split(content, "\n") {
						if pat.MatchString(line) {
							t.Errorf("%s:%d: forbidden scaffold claim (pattern %q): %s",
								rel, i+1, pat.String(), strings.TrimSpace(line))
						}
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("walking %s: %v", target, err)
		}
	}
}
