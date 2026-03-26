package metrics

import "testing"

func TestPathMatcher_Match(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     string
	}{
		{
			name:     "no patterns returns other",
			patterns: nil,
			path:     "/users/123",
			want:     "other",
		},
		{
			name:     "exact match single segment",
			patterns: []string{"/health"},
			path:     "/health",
			want:     "/health",
		},
		{
			name:     "exact match multi segment",
			patterns: []string{"/api/v1/status"},
			path:     "/api/v1/status",
			want:     "/api/v1/status",
		},
		{
			name:     "param match single param",
			patterns: []string{"/users/:id"},
			path:     "/users/123",
			want:     "/users/:id",
		},
		{
			name:     "param match multiple params",
			patterns: []string{"/api/v1/items/:item_id/comments/:comment_id"},
			path:     "/api/v1/items/42/comments/7",
			want:     "/api/v1/items/:item_id/comments/:comment_id",
		},
		{
			name:     "no match returns other",
			patterns: []string{"/users/:id"},
			path:     "/posts/456",
			want:     "other",
		},
		{
			name:     "segment count mismatch returns other",
			patterns: []string{"/users/:id"},
			path:     "/users/123/posts",
			want:     "other",
		},
		{
			name:     "first matching pattern wins",
			patterns: []string{"/users/:id", "/users/me"},
			path:     "/users/me",
			want:     "/users/:id",
		},
		{
			name:     "root path matches root pattern",
			patterns: []string{"/"},
			path:     "/",
			want:     "/",
		},
		{
			name:     "multiple patterns picks correct one",
			patterns: []string{"/users/:id", "/posts/:slug"},
			path:     "/posts/hello-world",
			want:     "/posts/:slug",
		},
		{
			name:     "path with no leading slash still matches",
			patterns: []string{"/users/:id"},
			path:     "users/123",
			want:     "/users/:id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := NewPathMatcher(tt.patterns)
			got := pm.Match(tt.path)
			if got != tt.want {
				t.Errorf("Match(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestNewPathMatcher_EmptyPatterns(t *testing.T) {
	pm := NewPathMatcher([]string{})
	got := pm.Match("/anything")
	if got != "other" {
		t.Errorf("empty PathMatcher.Match(%q) = %q, want %q", "/anything", got, "other")
	}
}
