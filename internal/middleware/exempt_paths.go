package middleware

import (
	"fmt"
	"path"
)

// exemptVibewardenPrefix is always exempt from rate limiting; it covers health
// checks and other internal VibeWarden endpoints.
const exemptVibewardenPrefix = "/_vibewarden/*"

// ExemptPathMatcher checks whether an HTTP request path should bypass rate
// limiting. Patterns follow stdlib path.Match syntax (single-segment wildcards
// only; use one pattern per path segment).
//
// The /_vibewarden/* prefix is always exempt and cannot be removed.
type ExemptPathMatcher struct {
	patterns []string
}

// NewExemptPathMatcher compiles the given glob patterns into an ExemptPathMatcher.
// The /_vibewarden/* prefix is always added automatically.
// Returns an error if any pattern is syntactically invalid according to
// path.Match rules.
func NewExemptPathMatcher(patterns []string) (*ExemptPathMatcher, error) {
	all := make([]string, 0, len(patterns)+1)
	all = append(all, exemptVibewardenPrefix)
	all = append(all, patterns...)

	// Validate each pattern eagerly so callers get a clear error at
	// construction time rather than on the first request.
	for _, p := range all {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid exempt path pattern %q: %w", p, err)
		}
	}

	return &ExemptPathMatcher{patterns: all}, nil
}

// Matches reports whether the given request path matches any exempt path
// pattern. A path is exempt if it matches at least one pattern.
func (m *ExemptPathMatcher) Matches(requestPath string) bool {
	for _, p := range m.patterns {
		matched, err := path.Match(p, requestPath)
		if err != nil {
			// Pattern was already validated in NewExemptPathMatcher; this
			// branch is unreachable in practice.
			continue
		}
		if matched {
			return true
		}
	}
	return false
}
