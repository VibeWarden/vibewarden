package metrics

import "strings"

// PathMatcher normalizes request paths to configured patterns to prevent
// high-cardinality labels in Prometheus metrics.
//
// Patterns use :param syntax for path parameters. For example:
//
//	"/users/:id"                            matches "/users/123"
//	"/api/v1/items/:item_id/comments/:cid"  matches "/api/v1/items/42/comments/7"
//
// Paths that do not match any pattern are returned as "other".
type PathMatcher struct {
	patterns []pathPattern
}

type pathPattern struct {
	original string   // e.g., "/users/:id"
	segments []string // e.g., ["users", ":id"]
}

// NewPathMatcher creates a PathMatcher from a list of pattern strings.
// Patterns use :param syntax for path parameters (e.g., "/users/:id").
// An empty slice produces a matcher that always returns "other".
func NewPathMatcher(patterns []string) *PathMatcher {
	pm := &PathMatcher{
		patterns: make([]pathPattern, 0, len(patterns)),
	}
	for _, p := range patterns {
		segments := strings.Split(strings.Trim(p, "/"), "/")
		pm.patterns = append(pm.patterns, pathPattern{
			original: p,
			segments: segments,
		})
	}
	return pm
}

// Match returns the matching pattern for a path, or "other" if no pattern matches.
// An exact segment match or a :param wildcard segment both satisfy a match.
func (pm *PathMatcher) Match(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")

	for _, pattern := range pm.patterns {
		if matchSegments(pattern.segments, segments) {
			return pattern.original
		}
	}
	return "other"
}

// matchSegments reports whether the given path segments match the pattern segments.
// A pattern segment beginning with ":" is a wildcard that matches any path segment.
func matchSegments(pattern, path []string) bool {
	if len(pattern) != len(path) {
		return false
	}
	for i, seg := range pattern {
		if strings.HasPrefix(seg, ":") {
			continue // wildcard matches anything
		}
		if seg != path[i] {
			return false
		}
	}
	return true
}
