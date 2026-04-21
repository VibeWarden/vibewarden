package cmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAddCommandsHaveNoMapRoundTrips enforces the invariant recorded in
// CLAUDE.md: every vibew add command that edits a YAML file must do so via
// the node-based yamlmod facade (which preserves comments and ordering).
//
// A map round-trip — yaml.Unmarshal into map[string]any followed by
// yaml.Marshal — silently destroys comments, blank lines, and key ordering.
// See issue #1086 for the motivating regression.
//
// This test greps every file under internal/cli/cmd/add_*.go and every
// non-test file under internal/app/scaffold/ for the forbidden pair:
// yaml.Unmarshal(..., &map[...]...) and yaml.Marshal(map[...]...).
//
// When it fires, the fix is to route the edit through
// yamlmod.UpsertFields or yamlmod.Toggler.EnableFeature.
func TestAddCommandsHaveNoMapRoundTrips(t *testing.T) {
	// Repository root — the test package lives four levels down:
	// <repo>/internal/cli/cmd/add_lint_test.go
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", "..", ".."))

	targets := []struct {
		dir  string
		glob string
	}{
		{filepath.Join(repoRoot, "internal", "cli", "cmd"), "add_*.go"},
		{filepath.Join(repoRoot, "internal", "app", "scaffold"), "add_*.go"},
	}

	// A file is a violation when it declares (or assigns to) a
	// map[string]any/interface{} AND calls yaml.Unmarshal AND calls
	// yaml.Marshal. That combination is the canonical round-trip that
	// drops comments and ordering.
	mapDecl := regexp.MustCompile(`map\[string\](?:any|interface\{\})`)
	unmarshalCall := regexp.MustCompile(`yaml\.Unmarshal\s*\(`)
	marshalCall := regexp.MustCompile(`yaml\.Marshal\s*\(`)

	var violations []string
	for _, tgt := range targets {
		matches, err := filepath.Glob(filepath.Join(tgt.dir, tgt.glob))
		if err != nil {
			t.Fatalf("glob %s/%s: %v", tgt.dir, tgt.glob, err)
		}
		for _, path := range matches {
			// Skip this lint test file and any other test file — tests
			// sometimes use map[string]any for fixtures where comments
			// do not matter.
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			b, err := os.ReadFile(path) //nolint:gosec // path comes from a filepath.Glob rooted at the repo
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			src := string(b)
			if mapDecl.MatchString(src) && unmarshalCall.MatchString(src) && marshalCall.MatchString(src) {
				violations = append(violations, path)
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("forbidden yaml map round-trip found — route the edit through internal/adapters/yamlmod instead:\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}
