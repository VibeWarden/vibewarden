package middleware

import (
	"fmt"
	"path"
)

const (
	// vibewardenPrefix is always public; it covers health checks and
	// other internal VibeWarden endpoints.
	vibewardenPrefix = "/_vibewarden/*"
)

// PublicPathMatcher checks whether an HTTP request path should bypass
// authentication. Patterns follow stdlib path.Match syntax (single-segment
// wildcards only; use one pattern per path segment).
type PublicPathMatcher struct {
	patterns []string
}

// NewPublicPathMatcher compiles the given glob patterns into a PublicPathMatcher.
// The /_vibewarden/* prefix is always added automatically.
// Returns an error if any pattern is syntactically invalid according to
// path.Match rules.
func NewPublicPathMatcher(patterns []string) (*PublicPathMatcher, error) {
	all := make([]string, 0, len(patterns)+1)
	all = append(all, vibewardenPrefix)
	all = append(all, patterns...)

	// Validate each pattern eagerly so callers get a clear error at
	// construction time rather than on the first request.
	for _, p := range all {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid public path pattern %q: %w", p, err)
		}
	}

	return &PublicPathMatcher{patterns: all}, nil
}

// Matches reports whether the given request path matches any public path
// pattern. A path is considered public if it matches at least one pattern.
func (m *PublicPathMatcher) Matches(requestPath string) bool {
	for _, p := range m.patterns {
		matched, err := path.Match(p, requestPath)
		if err != nil {
			// Pattern was already validated in NewPublicPathMatcher; this
			// branch is unreachable in practice.
			continue
		}
		if matched {
			return true
		}
	}
	return false
}
