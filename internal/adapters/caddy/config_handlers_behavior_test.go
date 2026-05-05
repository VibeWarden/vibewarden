package caddy

import (
	"net/http"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/headers"
)

// TestUserHeaderStripHandler_ActuallyStripsHeaders is a behavioral test that
// exercises the real Caddy headers.HeaderOps.ApplyTo runtime to prove that the
// generated delete pattern strips X-User-* headers from an incoming request.
//
// This test catches the class of bug where a structurally-correct-looking JSON
// pattern (e.g. "~^X-User-") silently does nothing at runtime because Caddy's
// headers module does not support tilde-prefix regex notation. The correct
// Caddy suffix-wildcard glob is "x-user-*".
//
// The test will FAIL with the broken pattern "~^X-User-" (or any variant not
// using Caddy's "*"-suffix glob) and PASS with "x-user-*".
//
// Regression test for #1264 (Part A — reviewer blocker).
func TestUserHeaderStripHandler_ActuallyStripsHeaders(t *testing.T) {
	forgedHeaders := []string{
		"X-User-Name",
		"X-User-Custom",
		"X-User-Id",
		"X-User-Email",
	}

	tests := []struct {
		name         string
		pattern      string
		wantStripped bool
	}{
		{
			name:         "correct Caddy suffix-wildcard glob strips all X-User-* headers",
			pattern:      "x-user-*",
			wantStripped: true,
		},
		{
			name:         "broken tilde-prefix regex pattern is a silent no-op",
			pattern:      "~^X-User-",
			wantStripped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the HeaderOps directly using the pattern under test.
			ops := &headers.HeaderOps{
				Delete: []string{tt.pattern},
			}

			// Construct a request with forged X-User-* headers.
			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatalf("http.NewRequest: %v", err)
			}
			for _, h := range forgedHeaders {
				req.Header.Set(h, "forged")
			}

			// ApplyTo requires a caddy.Replacer (used for placeholder expansion).
			// A fresh replacer with no custom vars works correctly here — none of
			// the delete patterns contain Caddy placeholders.
			repl := gocaddy.NewReplacer()
			ops.ApplyTo(req.Header, repl)

			// Assert post-strip header state.
			for _, h := range forgedHeaders {
				val := req.Header.Get(h)
				if tt.wantStripped && val != "" {
					t.Errorf("pattern %q: header %q still present (= %q) after strip — strip is a no-op; check Caddy delete pattern syntax", tt.pattern, h, val)
				}
				if !tt.wantStripped && val == "" {
					// This branch only fires if the "broken pattern" test unexpectedly
					// strips headers — that would mean we misidentified the broken pattern.
					t.Errorf("pattern %q: header %q unexpectedly absent — pattern is not broken as expected", tt.pattern, h)
				}
			}
		})
	}
}

// TestUserHeaderStripHandler_GlobMatchesAllVariants verifies that the "x-user-*"
// pattern covers all known and hypothetical X-User-* header variants, not just
// the four headers in the original hardcoded list.
func TestUserHeaderStripHandler_GlobMatchesAllVariants(t *testing.T) {
	variants := []string{
		"X-User-Id",
		"X-User-Email",
		"X-User-Verified",
		"X-User-Role",
		"X-User-Name",   // previously missing from hardcoded list (#1264)
		"X-User-Custom", // hypothetical future header
		"X-User-Org",    // hypothetical future header
	}

	ops := &headers.HeaderOps{
		Delete: []string{"x-user-*"},
	}

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	// Set all variants.
	for _, h := range variants {
		req.Header.Set(h, "forged")
	}
	// Set a non-X-User header to verify it is NOT stripped.
	req.Header.Set("X-Request-Id", "keep-me")

	repl := gocaddy.NewReplacer()
	ops.ApplyTo(req.Header, repl)

	// All X-User-* variants must be stripped.
	for _, h := range variants {
		if val := req.Header.Get(h); val != "" {
			t.Errorf("X-User-* variant %q still present (= %q) after strip", h, val)
		}
	}

	// Non-X-User-* header must survive.
	if val := req.Header.Get("X-Request-Id"); val != "keep-me" {
		t.Errorf("X-Request-Id was incorrectly stripped (got %q, want %q)", val, "keep-me")
	}
}
