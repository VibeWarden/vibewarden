//go:build integration

// Package middleware contains integration tests for the WAF middleware.
// These tests run the complete WAF middleware against real HTTP requests
// using attack payloads across both block and detect modes.
package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/waf"
)

// TestWAFMiddleware_Integration_BlockMode_AttackPayloads exercises the WAF
// middleware in block mode against a comprehensive set of attack payloads.
func TestWAFMiddleware_Integration_BlockMode_AttackPayloads(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		query       string
		body        string
		contentType string
		header      map[string]string
	}{
		// SQL Injection
		{name: "sqli tautology", method: "GET", path: "/search", query: "q=' OR '1'='1"},
		{name: "sqli union select", method: "GET", path: "/search", query: "q=1 UNION SELECT 1,2,3"},
		{name: "sqli drop table", method: "GET", path: "/admin", query: "cmd=DROP TABLE users"},
		{name: "sqli comment terminator", method: "GET", path: "/login", query: "user=admin--"},
		{name: "sqli stacked query", method: "GET", path: "/data", query: "id=1;SELECT 1"},
		// XSS
		{name: "xss script tag", method: "GET", path: "/page", query: "title=<script>alert(1)</script>"},
		{name: "xss javascript uri", method: "GET", path: "/link", query: "href=javascript:alert(1)"},
		{name: "xss event handler", method: "GET", path: "/img", query: "src=x onload=alert(1)"},
		// Path Traversal
		{name: "path traversal dotdot", method: "GET", path: "/files", query: "path=../../etc/passwd"},
		{name: "path traversal etc passwd", method: "GET", path: "/view", query: "file=/etc/passwd"},
		// Command Injection
		{name: "cmdi semicolon", method: "GET", path: "/run", query: "cmd=%3Bcat+/etc/passwd"},
		{name: "cmdi pipe", method: "GET", path: "/run", query: "cmd=%7Cid"},
		// Body payloads
		{
			name:        "body sqli",
			method:      "POST",
			path:        "/api/login",
			body:        `{"username":"admin' OR 1=1--","password":"x"}`,
			contentType: "application/json",
		},
		{
			name:        "body xss",
			method:      "POST",
			path:        "/api/comment",
			body:        `{"comment":"<script>document.cookie</script>"}`,
			contentType: "application/json",
		},
	}

	mc := &fakeWAFCollector{}
	rs := defaultRuleSet(t)
	mw := WAFMiddleware(rs, WAFConfig{Mode: WAFModeBlock}, newTestLogger(), mc, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.path
			if tt.query != "" {
				target = tt.path + "?" + tt.query
			}

			var bodyReader *strings.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			} else {
				bodyReader = strings.NewReader("")
			}

			req := httptest.NewRequest(tt.method, target, bodyReader)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403 Forbidden for attack %q, got %d", tt.name, rr.Code)
				return
			}

			var errResp ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
				t.Fatalf("decoding error response: %v", err)
			}
			if errResp.Error != "waf_blocked" {
				t.Errorf("error code = %q, want \"waf_blocked\"", errResp.Error)
			}
			if errResp.Status != http.StatusForbidden {
				t.Errorf("error status = %d, want 403", errResp.Status)
			}
		})
	}
}

// TestWAFMiddleware_Integration_DetectMode_AttackPayloads verifies that in
// detect mode all attack payloads pass through with HTTP 200 and are counted
// in the metrics.
func TestWAFMiddleware_Integration_DetectMode_AttackPayloads(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		query  string
	}{
		{name: "sqli union", method: "GET", path: "/search", query: "q=1 UNION SELECT 1,2,3"},
		{name: "xss script", method: "GET", path: "/page", query: "title=<script>alert(1)</script>"},
		{name: "path traversal", method: "GET", path: "/files", query: "path=../../etc/passwd"},
		{name: "cmdi pipe", method: "GET", path: "/run", query: "cmd=%7Cid"},
	}

	mc := &fakeWAFCollector{}
	rs := defaultRuleSet(t)
	mw := WAFMiddleware(rs, WAFConfig{Mode: WAFModeDetect}, newTestLogger(), mc, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path+"?"+tt.query, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("detect mode: expected 200 for attack %q, got %d", tt.name, rr.Code)
			}
		})
	}

	// All attacks should have been counted.
	if len(mc.detections) == 0 {
		t.Error("detect mode: expected detection metrics to be recorded")
	}
	for _, d := range mc.detections {
		if d.mode != "detect" {
			t.Errorf("detection metric mode = %q, want \"detect\"", d.mode)
		}
	}
}

// TestWAFMiddleware_Integration_PerRuleToggles verifies that disabling a
// category allows attacks of that category through while other categories
// are still blocked.
func TestWAFMiddleware_Integration_PerRuleToggles(t *testing.T) {
	mc := &fakeWAFCollector{}
	rs := defaultRuleSet(t)
	cfg := WAFConfig{
		Mode: WAFModeBlock,
		EnabledCategories: map[waf.Category]bool{
			waf.CategorySQLInjection:     false,
			waf.CategoryCommandInjection: false,
		},
	}
	mw := WAFMiddleware(rs, cfg, newTestLogger(), mc, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	disabled := []struct {
		name  string
		query string
	}{
		{"sqli union (disabled)", "q=1 UNION SELECT 1,2,3"},
		{"cmdi pipe (disabled)", "cmd=%7Cid"},
	}
	for _, tt := range disabled {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api?"+tt.query, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("disabled category should pass: status = %d, want 200", rr.Code)
			}
		})
	}

	enabled := []struct {
		name  string
		query string
	}{
		{"xss script (still enabled)", "title=<script>alert(1)</script>"},
		{"path traversal (still enabled)", "path=../../etc/passwd"},
	}
	for _, tt := range enabled {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api?"+tt.query, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Errorf("enabled category should block: status = %d, want 403", rr.Code)
			}
		})
	}
}
