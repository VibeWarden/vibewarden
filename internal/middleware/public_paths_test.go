package middleware

import (
	"testing"
)

func TestNewPublicPathMatcher_InvalidPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"valid wildcard", "/static/*", false},
		{"valid exact", "/health", false},
		{"valid nested wildcard", "/api/v1/*/public", false},
		// path.Match treats '[' as the start of a character class; an
		// unclosed bracket is a syntax error.
		{"invalid bracket", "/bad/[pattern", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPublicPathMatcher([]string{tt.pattern})
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPublicPathMatcher([%q]) error = %v, wantErr %v", tt.pattern, err, tt.wantErr)
			}
		})
	}
}

func TestPublicPathMatcher_Matches(t *testing.T) {
	tests := []struct {
		name         string
		patterns     []string
		requestPath  string
		wantMatch    bool
	}{
		// /_vibewarden/* is always added automatically.
		{
			name:        "vibewarden health always public",
			patterns:    []string{},
			requestPath: "/_vibewarden/health",
			wantMatch:   true,
		},
		{
			name:        "vibewarden sub-path always public",
			patterns:    []string{},
			requestPath: "/_vibewarden/metrics",
			wantMatch:   true,
		},
		{
			// path.Match treats * as matching zero or more characters within
			// a single path element, so "/_vibewarden/" (empty final element)
			// is matched by "/_vibewarden/*" and is therefore public.
			name:        "vibewarden prefix with trailing slash is public",
			patterns:    []string{},
			requestPath: "/_vibewarden/",
			wantMatch:   true,
		},
		{
			// "/_vibewarden" without trailing slash does NOT match
			// "/_vibewarden/*" — the wildcard requires the slash separator.
			name:        "vibewarden prefix bare without slash not matched",
			patterns:    []string{},
			requestPath: "/_vibewarden",
			wantMatch:   false,
		},
		// Static wildcard pattern.
		{
			name:        "static asset matches wildcard",
			patterns:    []string{"/static/*"},
			requestPath: "/static/app.js",
			wantMatch:   true,
		},
		{
			name:        "static sub-directory does not match single wildcard",
			patterns:    []string{"/static/*"},
			requestPath: "/static/css/main.css",
			wantMatch:   false,
		},
		// Exact match.
		{
			name:        "exact path matches",
			patterns:    []string{"/health"},
			requestPath: "/health",
			wantMatch:   true,
		},
		{
			name:        "different path does not match exact",
			patterns:    []string{"/health"},
			requestPath: "/dashboard",
			wantMatch:   false,
		},
		// Protected path (not in public list).
		{
			name:        "protected path not matched",
			patterns:    []string{"/health", "/static/*"},
			requestPath: "/dashboard",
			wantMatch:   false,
		},
		// Glob pattern with multiple segments.
		{
			name:        "api public segment matched",
			patterns:    []string{"/api/v1/*/public"},
			requestPath: "/api/v1/users/public",
			wantMatch:   true,
		},
		{
			name:        "api private segment not matched",
			patterns:    []string{"/api/v1/*/public"},
			requestPath: "/api/v1/users/private",
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewPublicPathMatcher(tt.patterns)
			if err != nil {
				t.Fatalf("NewPublicPathMatcher() unexpected error: %v", err)
			}

			got := m.Matches(tt.requestPath)
			if got != tt.wantMatch {
				t.Errorf("Matches(%q) = %v, want %v", tt.requestPath, got, tt.wantMatch)
			}
		})
	}
}
