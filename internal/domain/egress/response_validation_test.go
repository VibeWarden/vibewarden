package egress_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestResponseValidationConfig_IsZero verifies the zero-value detection.
func TestResponseValidationConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  egress.ResponseValidationConfig
		want bool
	}{
		{
			name: "empty config is zero",
			cfg:  egress.ResponseValidationConfig{},
			want: true,
		},
		{
			name: "status codes set — not zero",
			cfg:  egress.ResponseValidationConfig{StatusCodes: []string{"2xx"}},
			want: false,
		},
		{
			name: "content types set — not zero",
			cfg:  egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			want: false,
		},
		{
			name: "both set — not zero",
			cfg: egress.ResponseValidationConfig{
				StatusCodes:  []string{"2xx"},
				ContentTypes: []string{"application/json"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsZero()
			if got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResponseValidationConfig_MatchesStatusCode verifies status code matching.
func TestResponseValidationConfig_MatchesStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		cfg        egress.ResponseValidationConfig
		statusCode int
		want       bool
	}{
		// Empty allowlist — everything passes.
		{
			name:       "empty allowlist allows any code",
			cfg:        egress.ResponseValidationConfig{},
			statusCode: 500,
			want:       true,
		},
		// Class wildcards.
		{
			name:       "2xx matches 200",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx"}},
			statusCode: 200,
			want:       true,
		},
		{
			name:       "2xx matches 201",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx"}},
			statusCode: 201,
			want:       true,
		},
		{
			name:       "2xx matches 299",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx"}},
			statusCode: 299,
			want:       true,
		},
		{
			name:       "2xx does not match 300",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx"}},
			statusCode: 300,
			want:       false,
		},
		{
			name:       "3xx matches 301",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"3xx"}},
			statusCode: 301,
			want:       true,
		},
		{
			name:       "3xx does not match 200",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"3xx"}},
			statusCode: 200,
			want:       false,
		},
		{
			name:       "4xx matches 404",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"4xx"}},
			statusCode: 404,
			want:       true,
		},
		{
			name:       "5xx matches 500",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"5xx"}},
			statusCode: 500,
			want:       true,
		},
		{
			name:       "5xx does not match 200",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"5xx"}},
			statusCode: 200,
			want:       false,
		},
		// Exact codes.
		{
			name:       "exact 200 matches 200",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"200"}},
			statusCode: 200,
			want:       true,
		},
		{
			name:       "exact 200 does not match 201",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"200"}},
			statusCode: 201,
			want:       false,
		},
		// Multiple entries — OR semantics.
		{
			name:       "2xx or 301 — matches 200",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx", "301"}},
			statusCode: 200,
			want:       true,
		},
		{
			name:       "2xx or 301 — matches 301",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx", "301"}},
			statusCode: 301,
			want:       true,
		},
		{
			name:       "2xx or 301 — does not match 302",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"2xx", "301"}},
			statusCode: 302,
			want:       false,
		},
		// Unknown / garbage expression does not match.
		{
			name:       "unknown expression does not match",
			cfg:        egress.ResponseValidationConfig{StatusCodes: []string{"not-a-code"}},
			statusCode: 200,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.MatchesStatusCode(tt.statusCode)
			if got != tt.want {
				t.Errorf("MatchesStatusCode(%d) = %v, want %v", tt.statusCode, got, tt.want)
			}
		})
	}
}

// TestResponseValidationConfig_MatchesContentType verifies content-type matching.
func TestResponseValidationConfig_MatchesContentType(t *testing.T) {
	tests := []struct {
		name        string
		cfg         egress.ResponseValidationConfig
		contentType string
		want        bool
	}{
		// Empty allowlist — everything passes.
		{
			name:        "empty allowlist allows any content type",
			cfg:         egress.ResponseValidationConfig{},
			contentType: "text/html",
			want:        true,
		},
		// Exact match.
		{
			name:        "exact match application/json",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			contentType: "application/json",
			want:        true,
		},
		// Parameters are stripped.
		{
			name:        "match ignores charset parameter",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			contentType: "application/json; charset=utf-8",
			want:        true,
		},
		{
			name:        "match ignores boundary parameter",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"multipart/form-data"}},
			contentType: "multipart/form-data; boundary=----WebKitFormBoundary",
			want:        true,
		},
		// Case-insensitive.
		{
			name:        "case-insensitive match",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			contentType: "Application/JSON",
			want:        true,
		},
		// Mismatch.
		{
			name:        "text/html not in json allowlist",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			contentType: "text/html",
			want:        false,
		},
		// Empty content type — only matches when empty string is in allowlist.
		{
			name:        "empty content type does not match non-empty allowlist",
			cfg:         egress.ResponseValidationConfig{ContentTypes: []string{"application/json"}},
			contentType: "",
			want:        false,
		},
		// Multiple allowed types — OR semantics.
		{
			name: "json or text/plain — matches json",
			cfg: egress.ResponseValidationConfig{
				ContentTypes: []string{"application/json", "text/plain"},
			},
			contentType: "application/json",
			want:        true,
		},
		{
			name: "json or text/plain — matches text/plain",
			cfg: egress.ResponseValidationConfig{
				ContentTypes: []string{"application/json", "text/plain"},
			},
			contentType: "text/plain; charset=utf-8",
			want:        true,
		},
		{
			name: "json or text/plain — does not match text/html",
			cfg: egress.ResponseValidationConfig{
				ContentTypes: []string{"application/json", "text/plain"},
			},
			contentType: "text/html",
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.MatchesContentType(tt.contentType)
			if got != tt.want {
				t.Errorf("MatchesContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

// TestRoute_ValidateResponse verifies that WithValidateResponse is wired
// through NewRoute and returned by ValidateResponse.
func TestRoute_ValidateResponse(t *testing.T) {
	cfg := egress.ResponseValidationConfig{
		StatusCodes:  []string{"2xx", "301"},
		ContentTypes: []string{"application/json"},
	}
	route, err := egress.NewRoute("api", "https://api.example.com/*",
		egress.WithValidateResponse(cfg),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	got := route.ValidateResponse()
	if got.IsZero() {
		t.Error("ValidateResponse() should not be zero after WithValidateResponse")
	}
	if len(got.StatusCodes) != 2 {
		t.Errorf("StatusCodes len = %d, want 2", len(got.StatusCodes))
	}
	if len(got.ContentTypes) != 1 {
		t.Errorf("ContentTypes len = %d, want 1", len(got.ContentTypes))
	}
}

// TestRoute_ValidateResponse_Zero verifies that a route without the option
// returns the zero value.
func TestRoute_ValidateResponse_Zero(t *testing.T) {
	route, err := egress.NewRoute("api", "https://api.example.com/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	if !route.ValidateResponse().IsZero() {
		t.Error("ValidateResponse() should be zero when not set")
	}
}
