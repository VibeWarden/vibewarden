package authui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsSafeReturnTo verifies that isSafeReturnTo accepts only relative,
// same-origin paths and rejects all forms of open-redirect payload.
func TestIsSafeReturnTo(t *testing.T) {
	tests := []struct {
		name string
		v    string
		want bool
	}{
		// Valid relative paths.
		{name: "root path", v: "/", want: true},
		{name: "dashboard path", v: "/dashboard", want: true},
		{name: "nested path", v: "/admin/users", want: true},
		{name: "path with query", v: "/path?q=1", want: true},
		{name: "path with fragment", v: "/path#section", want: true},
		{name: "path with encoded chars", v: "/path%20with%20spaces", want: true},

		// Absolute external URLs — must be rejected.
		{name: "https external", v: "https://evil.com", want: false},
		{name: "http external", v: "http://evil.com", want: false},
		{name: "ftp scheme", v: "ftp://evil.com/path", want: false},
		{name: "javascript scheme", v: "javascript:alert(1)", want: false},
		{name: "javascript uppercase", v: "JAVASCRIPT:alert(1)", want: false},
		{name: "data URI", v: "data:text/html,<h1>evil</h1>", want: false},
		{name: "vbscript scheme", v: "vbscript:msgbox(1)", want: false},

		// Protocol-relative URLs — browsers treat these as absolute.
		{name: "protocol-relative double slash", v: "//evil.com", want: false},
		{name: "protocol-relative with path", v: "//evil.com/path", want: false},

		// Backslash tricks — some browsers interpret /\ as //.
		{name: "backslash trick", v: "/\\evil.com", want: false},
		{name: "backslash with path", v: "/\\evil.com/path", want: false},

		// HTTP response-splitting / log-injection variants.
		{name: "newline injection", v: "/path\nhttp://evil.com", want: false},
		{name: "carriage return injection", v: "/path\rhttp://evil.com", want: false},
		{name: "crlf injection", v: "/path\r\nSet-Cookie: x=y", want: false},

		// Values without leading slash.
		{name: "no leading slash", v: "evil.com", want: false},
		{name: "relative without slash", v: "dashboard", want: false},

		// Empty value.
		{name: "empty string", v: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeReturnTo(tt.v)
			if got != tt.want {
				t.Errorf("isSafeReturnTo(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// TestReturnToQuery_ServerSide verifies that returnToQuery propagates safe
// return_to values and drops unsafe ones.
func TestReturnToQuery_ServerSide(t *testing.T) {
	tests := []struct {
		name      string
		returnTo  string
		wantQuery string
	}{
		{
			name:      "safe path propagated",
			returnTo:  "/dashboard",
			wantQuery: "?return_to=/dashboard",
		},
		{
			name:      "safe root path propagated",
			returnTo:  "/",
			wantQuery: "?return_to=/",
		},
		{
			name:      "safe path with query params propagated",
			returnTo:  "/search?q=hello",
			wantQuery: "?return_to=/search?q=hello",
		},
		{
			name:      "external https URL rejected",
			returnTo:  "https://evil.com",
			wantQuery: "",
		},
		{
			name:      "protocol-relative URL rejected",
			returnTo:  "//evil.com",
			wantQuery: "",
		},
		{
			name:      "backslash trick rejected",
			returnTo:  "/\\evil.com",
			wantQuery: "",
		},
		{
			name:      "javascript scheme rejected",
			returnTo:  "javascript:alert(1)",
			wantQuery: "",
		},
		{
			name:      "newline injection rejected",
			returnTo:  "/path\nhttp://evil.com",
			wantQuery: "",
		},
		{
			name:      "empty return_to yields empty query",
			returnTo:  "",
			wantQuery: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build request by setting RawQuery directly to avoid httptest.NewRequest
			// panicking on control characters (newlines) in the URL string.
			r := httptest.NewRequest(http.MethodGet, "/_vibewarden/login", nil)
			if tt.returnTo != "" {
				// Use url.Values to encode the value exactly as a browser would
				// send it, then override with the raw value so control-char cases
				// actually reach the validation logic unencoded.
				q := r.URL.Query()
				q.Set("return_to", tt.returnTo)
				r.URL.RawQuery = q.Encode()
				// For payloads containing control characters the encoded form
				// would be percent-escaped and never reach isSafeReturnTo's
				// newline check via Query().Get(). Simulate an attacker who
				// bypasses encoding by setting RawQuery directly.
				if tt.returnTo == "/path\nhttp://evil.com" {
					r.URL.RawQuery = "return_to=/path%0ahttp://evil.com"
				}
			}
			got := returnToQuery(r)
			if got != tt.wantQuery {
				t.Errorf("returnToQuery(%q) = %q, want %q", tt.returnTo, got, tt.wantQuery)
			}
		})
	}
}

// TestReturnToQuery_ExternalURLDroppedFromPage verifies that when an external
// return_to is supplied, the rendered login and registration pages do NOT
// contain the external URL in their body — the server-side guard must strip it
// before template execution so no cross-site link survives into the HTML.
func TestReturnToQuery_ExternalURLDroppedFromPage(t *testing.T) {
	malicious := []string{
		"https://evil.com",
		"//evil.com",
		"/\\evil.com",
		"javascript:alert(1)",
	}

	pages := []struct {
		name string
		path string
	}{
		{name: "login", path: "/_vibewarden/login"},
		{name: "registration", path: "/_vibewarden/registration"},
	}

	for _, pg := range pages {
		for _, payload := range malicious {
			t.Run(pg.name+"/"+payload, func(t *testing.T) {
				h, err := NewHandler(AuthUIConfig{}, nil)
				if err != nil {
					t.Fatalf("NewHandler: %v", err)
				}
				if err := h.Start(); err != nil {
					t.Fatalf("Start: %v", err)
				}
				defer h.Stop(context.TODO()) //nolint:errcheck

				resp, err := http.Get("http://" + h.Addr() + pg.path + "?return_to=" + payload)
				if err != nil {
					t.Fatalf("GET: %v", err)
				}
				defer resp.Body.Close() //nolint:errcheck

				var buf [32768]byte
				n, _ := resp.Body.Read(buf[:])
				body := string(buf[:n])

				// The external payload must not appear anywhere in the page HTML.
				if contains(body, payload) {
					t.Errorf("page %s leaked external return_to %q into body", pg.path, payload)
				}
			})
		}
	}
}

// contains is a substring check to avoid importing strings in this file.
func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
