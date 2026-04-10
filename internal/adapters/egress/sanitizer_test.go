package egress

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// TestStripQueryParams verifies that named query parameters are removed from
// the URL and the correct count is returned.
func TestStripQueryParams(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		params    []string
		wantURL   string
		wantCount int
	}{
		{
			name:      "single param stripped",
			rawURL:    "https://api.example.com/v1/charge?api_key=secret&amount=100",
			params:    []string{"api_key"},
			wantURL:   "https://api.example.com/v1/charge?amount=100",
			wantCount: 1,
		},
		{
			name:      "multiple params stripped",
			rawURL:    "https://api.example.com/v1/charge?api_key=secret&token=abc&amount=100",
			params:    []string{"api_key", "token"},
			wantURL:   "https://api.example.com/v1/charge?amount=100",
			wantCount: 2,
		},
		{
			name:      "param not present — no change",
			rawURL:    "https://api.example.com/v1/charge?amount=100",
			params:    []string{"api_key"},
			wantURL:   "https://api.example.com/v1/charge?amount=100",
			wantCount: 0,
		},
		{
			name:      "all params stripped — empty query string",
			rawURL:    "https://api.example.com/v1/charge?api_key=secret",
			params:    []string{"api_key"},
			wantURL:   "https://api.example.com/v1/charge",
			wantCount: 1,
		},
		{
			name:      "URL with no query string — no change",
			rawURL:    "https://api.example.com/v1/charge",
			params:    []string{"api_key"},
			wantURL:   "https://api.example.com/v1/charge",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotCount, err := stripQueryParams(tt.rawURL, tt.params)
			if err != nil {
				t.Fatalf("stripQueryParams returned unexpected error: %v", err)
			}
			if gotCount != tt.wantCount {
				t.Errorf("count = %d, want %d", gotCount, tt.wantCount)
			}
			// Compare parsed query strings to be order-independent.
			if !queryEqual(gotURL, tt.wantURL) {
				t.Errorf("URL = %q, want %q", gotURL, tt.wantURL)
			}
		})
	}
}

// queryEqual compares two URLs by their parsed query strings, ignoring
// parameter order (url.Values.Encode sorts keys alphabetically).
func queryEqual(a, b string) bool {
	// Simple string comparison works here because url.Values.Encode is
	// deterministic (sorted), but we just do path+query comparison.
	return a == b || normalizeURL(a) == normalizeURL(b)
}

func normalizeURL(raw string) string {
	// Strip trailing "?" when query is empty.
	if strings.HasSuffix(raw, "?") {
		return raw[:len(raw)-1]
	}
	return raw
}

// TestRedactJSONFields verifies that JSON body field values are replaced with
// "[REDACTED]" and the correct count is returned.
func TestRedactJSONFields(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		fields    []string
		wantBody  string
		wantCount int
	}{
		{
			name:      "single top-level string field",
			body:      `{"username":"alice","password":"s3cr3t"}`,
			fields:    []string{"password"},
			wantBody:  `{"username":"alice","password":"[REDACTED]"}`,
			wantCount: 1,
		},
		{
			name:      "multiple fields",
			body:      `{"ssn":"123-45-6789","card":"4111111111111111","amount":100}`,
			fields:    []string{"ssn", "card"},
			wantBody:  `{"ssn":"[REDACTED]","card":"[REDACTED]","amount":100}`,
			wantCount: 2,
		},
		{
			name:      "field not present — no change",
			body:      `{"username":"alice"}`,
			fields:    []string{"password"},
			wantBody:  `{"username":"alice"}`,
			wantCount: 0,
		},
		{
			name:      "nested field is also redacted",
			body:      `{"user":{"password":"secret","name":"bob"}}`,
			fields:    []string{"password"},
			wantBody:  `{"user":{"password":"[REDACTED]","name":"bob"}}`,
			wantCount: 1,
		},
		{
			name:      "empty body — no change",
			body:      `{}`,
			fields:    []string{"password"},
			wantBody:  `{}`,
			wantCount: 0,
		},
		{
			name:      "field with whitespace around colon",
			body:      `{"password" : "s3cr3t","user":"alice"}`,
			fields:    []string{"password"},
			wantBody:  `{"password" : "[REDACTED]","user":"alice"}`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, count := redactJSONFields([]byte(tt.body), tt.fields)
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
			if string(got) != tt.wantBody {
				t.Errorf("body = %q, want %q", string(got), tt.wantBody)
			}
		})
	}
}

// TestIsJSONContentType verifies Content-Type detection.
func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/json;charset=utf-8", true},
		{"text/plain", false},
		{"application/x-www-form-urlencoded", false},
		{"", false},
		{"  application/json", true}, // leading space is trimmed
	}

	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			got := isJSONContentType(tt.ct)
			if got != tt.want {
				t.Errorf("isJSONContentType(%q) = %v, want %v", tt.ct, got, tt.want)
			}
		})
	}
}

// TestSanitizeRequest_HeadersRedactedInLogCopy verifies that sensitive header
// values are replaced in the returned log copy but preserved in the forwarded
// request headers.
func TestSanitizeRequest_HeadersRedactedInLogCopy(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer token123"},
		"Cookie":        []string{"session=abc"},
		"X-Custom":      []string{"visible"},
	}
	req, err := domainegress.NewEgressRequest("GET", "https://api.example.com/v1/resource", headers, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{
		Headers: []string{"Authorization", "Cookie"},
	}

	_, logHeaders, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.RedactedHeaders != 2 {
		t.Errorf("RedactedHeaders = %d, want 2", result.RedactedHeaders)
	}

	// Log copy must have redacted values.
	if got := logHeaders.Get("Authorization"); got != redacted {
		t.Errorf("logHeaders[Authorization] = %q, want %q", got, redacted)
	}
	if got := logHeaders.Get("Cookie"); got != redacted {
		t.Errorf("logHeaders[Cookie] = %q, want %q", got, redacted)
	}
	// Non-sensitive header must pass through unchanged.
	if got := logHeaders.Get("X-Custom"); got != "visible" {
		t.Errorf("logHeaders[X-Custom] = %q, want %q", got, "visible")
	}

	// Original request headers must be preserved.
	if got := req.Header.Get("Authorization"); got != "Bearer token123" {
		t.Errorf("req.Header[Authorization] = %q, want original value", got)
	}
}

// TestSanitizeRequest_QueryParamsStripped verifies that the URL has named
// query parameters removed before forwarding.
func TestSanitizeRequest_QueryParamsStripped(t *testing.T) {
	req, err := domainegress.NewEgressRequest(
		"GET",
		"https://api.example.com/search?api_key=secret&q=hello",
		nil, nil,
	)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{QueryParams: []string{"api_key"}}
	modified, _, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.StrippedQueryParams != 1 {
		t.Errorf("StrippedQueryParams = %d, want 1", result.StrippedQueryParams)
	}
	if strings.Contains(modified.URL, "api_key") {
		t.Errorf("URL still contains api_key: %s", modified.URL)
	}
	if !strings.Contains(modified.URL, "q=hello") {
		t.Errorf("URL is missing q=hello: %s", modified.URL)
	}
}

// TestSanitizeRequest_BodyFieldsRedacted verifies that JSON body field values
// are replaced in the forwarded request body.
func TestSanitizeRequest_BodyFieldsRedacted(t *testing.T) {
	body := `{"email":"user@example.com","password":"hunter2","amount":100}`
	headers := http.Header{"Content-Type": []string{"application/json"}}
	req, err := domainegress.NewEgressRequest(
		"POST",
		"https://api.example.com/v1/login",
		headers,
		io.NopCloser(strings.NewReader(body)),
	)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{BodyFields: []string{"password"}}
	modified, _, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.RedactedBodyFields != 1 {
		t.Errorf("RedactedBodyFields = %d, want 1", result.RedactedBodyFields)
	}

	bodyReader, ok := modified.BodyRef.(io.Reader)
	if !ok || bodyReader == nil {
		t.Fatal("BodyRef is not a readable io.Reader")
	}
	got, _ := io.ReadAll(bodyReader)
	if strings.Contains(string(got), "hunter2") {
		t.Errorf("body still contains the original password: %s", string(got))
	}
	if !strings.Contains(string(got), redacted) {
		t.Errorf("body does not contain %q: %s", redacted, string(got))
	}
}

// TestSanitizeRequest_NonJSONBodyNotTouched verifies that body redaction is
// skipped when the Content-Type is not application/json.
func TestSanitizeRequest_NonJSONBodyNotTouched(t *testing.T) {
	body := "password=hunter2&user=alice"
	headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}}
	req, err := domainegress.NewEgressRequest(
		"POST",
		"https://api.example.com/v1/login",
		headers,
		io.NopCloser(strings.NewReader(body)),
	)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{BodyFields: []string{"password"}}
	modified, _, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.RedactedBodyFields != 0 {
		t.Errorf("RedactedBodyFields = %d, want 0 for non-JSON body", result.RedactedBodyFields)
	}

	bodyReader, ok := modified.BodyRef.(io.Reader)
	if !ok || bodyReader == nil {
		t.Fatal("BodyRef is not a readable io.Reader after non-JSON bypass")
	}
	got, _ := io.ReadAll(bodyReader)
	if string(got) != body {
		t.Errorf("body was unexpectedly modified: %q", string(got))
	}
}

// TestSanitizeRequest_ZeroConfig is a no-op fast path check.
func TestSanitizeRequest_ZeroConfig(t *testing.T) {
	req, err := domainegress.NewEgressRequest("GET", "https://api.example.com/v1/resource", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, _, result, err := sanitizeRequest(context.Background(), req, domainegress.SanitizeConfig{})
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.Total() != 0 {
		t.Errorf("Total() = %d, want 0 for zero config", result.Total())
	}
}

// TestSanitizeResult_Total verifies that Total() sums all counters.
func TestSanitizeResult_Total(t *testing.T) {
	r := SanitizeResult{RedactedHeaders: 2, StrippedQueryParams: 3, RedactedBodyFields: 1}
	if got := r.Total(); got != 6 {
		t.Errorf("Total() = %d, want 6", got)
	}
}

// TestSanitizeRequest_BodyNilNotTouched verifies that a nil body is handled gracefully.
func TestSanitizeRequest_BodyNilNotTouched(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	req, err := domainegress.NewEgressRequest("POST", "https://api.example.com/v1/op", headers, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{BodyFields: []string{"password"}}
	_, _, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}
	if result.RedactedBodyFields != 0 {
		t.Errorf("RedactedBodyFields = %d for nil body, want 0", result.RedactedBodyFields)
	}
}

// TestSanitizeRequest_BodyBytesReader verifies that a *bytes.Reader body is handled.
func TestSanitizeRequest_BodyBytesReader(t *testing.T) {
	body := `{"token":"abc123","action":"login"}`
	headers := http.Header{"Content-Type": []string{"application/json"}}
	req, err := domainegress.NewEgressRequest(
		"POST",
		"https://api.example.com/v1/auth",
		headers,
		bytes.NewReader([]byte(body)),
	)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	cfg := domainegress.SanitizeConfig{BodyFields: []string{"token"}}
	modified, _, result, err := sanitizeRequest(context.Background(), req, cfg)
	if err != nil {
		t.Fatalf("sanitizeRequest: %v", err)
	}

	if result.RedactedBodyFields != 1 {
		t.Errorf("RedactedBodyFields = %d, want 1", result.RedactedBodyFields)
	}
	got, _ := io.ReadAll(modified.BodyRef.(io.Reader))
	if strings.Contains(string(got), "abc123") {
		t.Errorf("body still contains original token: %s", string(got))
	}
}
