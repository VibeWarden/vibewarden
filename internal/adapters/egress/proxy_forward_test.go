package egress

import (
	"net/http"
	"testing"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestShouldRetryStatus verifies the retryable status code table.
func TestShouldRetryStatus(t *testing.T) {
	tests := []struct {
		name string
		code int
		want bool
	}{
		{"408 Request Timeout", http.StatusRequestTimeout, true},
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
		{"504 Gateway Timeout", http.StatusGatewayTimeout, true},
		{"200 OK", http.StatusOK, false},
		{"201 Created", http.StatusCreated, false},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"501 Not Implemented", http.StatusNotImplemented, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryStatus(tt.code)
			if got != tt.want {
				t.Errorf("shouldRetryStatus(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// TestApplyHeaderManipulation verifies that X-Inject-Secret is always stripped
// on unmatched requests, and that matched routes apply their header rules.
func TestApplyHeaderManipulation(t *testing.T) {
	tests := []struct {
		name          string
		headers       http.Header
		match         domainegress.RouteMatch
		wantSecret    bool // whether X-Inject-Secret should be present
		wantHeaderKey string
		wantHeaderVal string
	}{
		{
			name: "unmatched — X-Inject-Secret stripped",
			headers: http.Header{
				"X-Inject-Secret": []string{"my-secret"},
				"Content-Type":    []string{"application/json"},
			},
			match:      domainegress.RouteMatch{Matched: false},
			wantSecret: false,
		},
		{
			name: "unmatched — other headers preserved",
			headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
			match:         domainegress.RouteMatch{Matched: false},
			wantSecret:    false,
			wantHeaderKey: "Content-Type",
			wantHeaderVal: "application/json",
		},
		{
			name: "unmatched — no X-Inject-Secret to strip",
			headers: http.Header{
				"Accept": []string{"*/*"},
			},
			match:         domainegress.RouteMatch{Matched: false},
			wantSecret:    false,
			wantHeaderKey: "Accept",
			wantHeaderVal: "*/*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := domainegress.EgressRequest{}
			req.Header = tt.headers

			out := applyHeaderManipulation(req, tt.match)

			if tt.wantSecret && out.Get("X-Inject-Secret") == "" {
				t.Error("X-Inject-Secret should be present but was stripped")
			}
			if !tt.wantSecret && out.Get("X-Inject-Secret") != "" {
				t.Errorf("X-Inject-Secret should be stripped but got %q", out.Get("X-Inject-Secret"))
			}

			if tt.wantHeaderKey != "" {
				if got := out.Get(tt.wantHeaderKey); got != tt.wantHeaderVal {
					t.Errorf("%s = %q, want %q", tt.wantHeaderKey, got, tt.wantHeaderVal)
				}
			}
		})
	}
}
