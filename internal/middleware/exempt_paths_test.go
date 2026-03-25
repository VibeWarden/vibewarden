package middleware

import (
	"testing"
)

func TestNewExemptPathMatcher_InvalidPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"valid wildcard", "/static/*", false},
		{"valid exact", "/healthz", false},
		{"valid nested wildcard", "/api/v1/*/public", false},
		// path.Match treats '[' as the start of a character class; an
		// unclosed bracket is a syntax error.
		{"invalid bracket", "/bad/[pattern", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewExemptPathMatcher([]string{tt.pattern})
			if (err != nil) != tt.wantErr {
				t.Errorf("NewExemptPathMatcher([%q]) error = %v, wantErr %v", tt.pattern, err, tt.wantErr)
			}
		})
	}
}

func TestExemptPathMatcher_Matches(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		requestPath string
		wantMatch   bool
	}{
		// /_vibewarden/* is always added automatically.
		{
			name:        "vibewarden health always exempt",
			patterns:    nil,
			requestPath: "/_vibewarden/health",
			wantMatch:   true,
		},
		{
			name:        "vibewarden sub-path always exempt",
			patterns:    nil,
			requestPath: "/_vibewarden/metrics",
			wantMatch:   true,
		},
		{
			name:        "vibewarden prefix with trailing slash is exempt",
			patterns:    nil,
			requestPath: "/_vibewarden/",
			wantMatch:   true,
		},
		{
			// "/_vibewarden" without trailing slash does NOT match
			// "/_vibewarden/*" — the wildcard requires the slash separator.
			name:        "vibewarden prefix bare without slash not matched",
			patterns:    nil,
			requestPath: "/_vibewarden",
			wantMatch:   false,
		},
		// Custom glob pattern.
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
			patterns:    []string{"/healthz"},
			requestPath: "/healthz",
			wantMatch:   true,
		},
		{
			name:        "different path does not match exact",
			patterns:    []string{"/healthz"},
			requestPath: "/dashboard",
			wantMatch:   false,
		},
		// Protected path (not in exempt list).
		{
			name:        "protected path not matched",
			patterns:    []string{"/healthz", "/static/*"},
			requestPath: "/api/users",
			wantMatch:   false,
		},
		// Multi-segment glob.
		{
			name:        "multi-segment glob matches",
			patterns:    []string{"/api/v1/*/public"},
			requestPath: "/api/v1/users/public",
			wantMatch:   true,
		},
		{
			name:        "multi-segment glob does not match private segment",
			patterns:    []string{"/api/v1/*/public"},
			requestPath: "/api/v1/users/private",
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewExemptPathMatcher(tt.patterns)
			if err != nil {
				t.Fatalf("NewExemptPathMatcher() unexpected error: %v", err)
			}

			got := m.Matches(tt.requestPath)
			if got != tt.wantMatch {
				t.Errorf("Matches(%q) = %v, want %v", tt.requestPath, got, tt.wantMatch)
			}
		})
	}
}
