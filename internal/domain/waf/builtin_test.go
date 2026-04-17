package waf

import "testing"

// TestBuiltinRules_Compile verifies that every built-in rule spec compiles
// successfully and carries non-empty metadata (name, category, severity).
// This is a compile-time invariant — builtinSpecs are hardcoded literals
// that must all produce valid Rules. Previously only exercised transitively
// via ruleset_test.go; making the check direct catches any new malformed
// spec at the earliest possible moment.
func TestBuiltinRules_Compile(t *testing.T) {
	rules := BuiltinRules()
	if len(rules) == 0 {
		t.Fatal("BuiltinRules() returned no rules — at least the SQLi/XSS/path-traversal rules should exist")
	}

	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if r.Name() == "" {
			t.Error("built-in rule has empty Name()")
		}
		if r.Category() == "" {
			t.Errorf("built-in rule %q has empty Category()", r.Name())
		}
		if r.Severity() == "" {
			t.Errorf("built-in rule %q has empty Severity()", r.Name())
		}
		if seen[r.Name()] {
			t.Errorf("duplicate built-in rule name %q", r.Name())
		}
		seen[r.Name()] = true
	}
}
