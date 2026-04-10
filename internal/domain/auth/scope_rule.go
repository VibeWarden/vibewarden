package auth

import (
	"fmt"
	"path"
)

// ScopeRule describes an authorization rule that restricts which API key scopes
// are required to access a particular path pattern and set of HTTP methods.
//
// Rules are evaluated after successful API key authentication. The first rule
// whose path pattern and method set match the incoming request determines the
// required scopes. When no rule matches, the request is allowed (open by default).
type ScopeRule struct {
	// Path is a glob pattern (stdlib path.Match syntax) matched against the
	// request URL path. A trailing "/*" covers all sub-paths of a prefix.
	// Example: "/api/v1/*"
	Path string

	// Methods is the set of HTTP methods this rule applies to. When empty,
	// the rule applies to all HTTP methods.
	Methods []string

	// RequiredScopes is the non-empty set of scope strings that the API key
	// must possess for the request to be allowed.
	RequiredScopes []string
}

// Matches reports whether this rule applies to the given HTTP method and
// request path. A rule matches when:
//   - the path pattern (ScopeRule.Path) matches requestPath via path.Match, AND
//   - either ScopeRule.Methods is empty or method is listed in ScopeRule.Methods.
//
// An invalid path pattern never matches; callers should validate rules at
// construction time via ValidateScopeRules.
func (r ScopeRule) Matches(method, requestPath string) bool {
	matched, err := path.Match(r.Path, requestPath)
	if err != nil || !matched {
		return false
	}
	if len(r.Methods) == 0 {
		return true
	}
	for _, m := range r.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// SatisfiedBy reports whether the given key scopes satisfy the rule's
// RequiredScopes. All required scopes must be present in keyScopes.
func (r ScopeRule) SatisfiedBy(keyScopes []Scope) bool {
	for _, required := range r.RequiredScopes {
		found := false
		for _, held := range keyScopes {
			if string(held) == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// MatchingScopeRule returns the first ScopeRule in rules that matches the given
// method and requestPath. It returns (ScopeRule{}, false) when no rule matches.
func MatchingScopeRule(rules []ScopeRule, method, requestPath string) (ScopeRule, bool) {
	for _, r := range rules {
		if r.Matches(method, requestPath) {
			return r, true
		}
	}
	return ScopeRule{}, false
}

// ValidateScopeRules returns an error if any rule contains a syntactically
// invalid path pattern. Validation is cheap and should be called at startup.
func ValidateScopeRules(rules []ScopeRule) error {
	for _, r := range rules {
		if _, err := path.Match(r.Path, ""); err != nil {
			return fmt.Errorf("invalid scope rule path pattern %q: %w", r.Path, err)
		}
	}
	return nil
}
