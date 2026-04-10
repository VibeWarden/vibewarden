package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newInputValidationRequest builds an httptest.Request with the given method,
// URL (path+query) and optional headers. It also sets RequestURI to simulate
// the full raw URI the server sees.
func newInputValidationRequest(t *testing.T, method, rawURL string, headers map[string]string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, rawURL, nil)
	// httptest.NewRequest populates URL but not RequestURI for non-server use.
	// Set it explicitly so URL-length checks work correctly.
	req.RequestURI = rawURL
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// inputValidationBody decodes the JSON error response body from the recorder.
func inputValidationBody(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	return resp
}

// noopHandler is a simple handler that records it was called.
func noopHandlerFunc(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

// ---------------------------------------------------------------------------
// Disabled middleware
// ---------------------------------------------------------------------------

func TestInputValidation_Disabled(t *testing.T) {
	called := false
	handler := InputValidation(InputValidationConfig{Enabled: false})(noopHandlerFunc(&called))

	req := newInputValidationRequest(t, http.MethodGet, "/api?"+strings.Repeat("a", 5000), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected next handler to be called when middleware is disabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Exempt paths
// ---------------------------------------------------------------------------

func TestInputValidation_ExemptVibewardenPaths(t *testing.T) {
	cfg := InputValidationConfig{
		Enabled:      true,
		MaxURLLength: 10, // tiny limit
	}

	called := false
	handler := InputValidation(cfg)(noopHandlerFunc(&called))

	// /_vibewarden/health has a very long fake URI but must pass through.
	req := newInputValidationRequest(t, http.MethodGet, "/_vibewarden/health?x=1", nil)
	// Artificially lengthen the RequestURI to exceed the limit.
	req.RequestURI = "/_vibewarden/health?" + strings.Repeat("x", 100)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected /_vibewarden/* to be exempt from input validation")
	}
}

// ---------------------------------------------------------------------------
// URL length
// ---------------------------------------------------------------------------

func TestInputValidation_URLLength(t *testing.T) {
	tests := []struct {
		name         string
		maxURLLength int
		rawURI       string
		wantStatus   int
	}{
		{
			name:         "within limit",
			maxURLLength: 2048,
			rawURI:       "/api?" + strings.Repeat("a", 10),
			wantStatus:   http.StatusOK,
		},
		{
			name:         "at limit",
			maxURLLength: 20,
			rawURI:       strings.Repeat("a", 20),
			wantStatus:   http.StatusOK,
		},
		{
			name:         "exceeds limit",
			maxURLLength: 20,
			rawURI:       strings.Repeat("a", 21),
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "zero disables check",
			maxURLLength: 0,
			rawURI:       "/" + strings.Repeat("a", 10000),
			wantStatus:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := InputValidationConfig{
				Enabled:      true,
				MaxURLLength: tt.maxURLLength,
			}
			called := false
			handler := InputValidation(cfg)(noopHandlerFunc(&called))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RequestURI = tt.rawURI
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusBadRequest {
				body := inputValidationBody(t, rec)
				if body.Error != "input_validation_failed" {
					t.Errorf("expected error code 'input_validation_failed', got %q", body.Error)
				}
				if called {
					t.Error("next handler must not be called when validation fails")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Query string length
// ---------------------------------------------------------------------------

func TestInputValidation_QueryStringLength(t *testing.T) {
	tests := []struct {
		name                 string
		maxQueryStringLength int
		query                string
		wantStatus           int
	}{
		{
			name:                 "within limit",
			maxQueryStringLength: 2048,
			query:                strings.Repeat("a", 100),
			wantStatus:           http.StatusOK,
		},
		{
			name:                 "at limit",
			maxQueryStringLength: 50,
			query:                strings.Repeat("b", 50),
			wantStatus:           http.StatusOK,
		},
		{
			name:                 "exceeds limit",
			maxQueryStringLength: 50,
			query:                strings.Repeat("b", 51),
			wantStatus:           http.StatusBadRequest,
		},
		{
			name:                 "zero disables check",
			maxQueryStringLength: 0,
			query:                strings.Repeat("z", 99999),
			wantStatus:           http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := InputValidationConfig{
				Enabled:              true,
				MaxQueryStringLength: tt.maxQueryStringLength,
			}
			called := false
			handler := InputValidation(cfg)(noopHandlerFunc(&called))

			req := httptest.NewRequest(http.MethodGet, "/path?"+tt.query, nil)
			req.RequestURI = "/path?" + tt.query
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusBadRequest {
				body := inputValidationBody(t, rec)
				if body.Error != "input_validation_failed" {
					t.Errorf("expected error code 'input_validation_failed', got %q", body.Error)
				}
				if called {
					t.Error("next handler must not be called when validation fails")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Header count
// ---------------------------------------------------------------------------

func TestInputValidation_HeaderCount(t *testing.T) {
	tests := []struct {
		name           string
		maxHeaderCount int
		headerCount    int
		wantStatus     int
	}{
		{
			name:           "within limit",
			maxHeaderCount: 100,
			headerCount:    5,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "at limit",
			maxHeaderCount: 3,
			headerCount:    3,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "exceeds limit",
			maxHeaderCount: 3,
			headerCount:    4,
			wantStatus:     http.StatusBadRequest,
		},
		{
			name:           "zero disables check",
			maxHeaderCount: 0,
			headerCount:    500,
			wantStatus:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := InputValidationConfig{
				Enabled:        true,
				MaxHeaderCount: tt.maxHeaderCount,
			}
			called := false
			handler := InputValidation(cfg)(noopHandlerFunc(&called))

			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.RequestURI = "/path"
			// Clear headers set by httptest so we control the count exactly.
			req.Header = make(http.Header)
			for i := 0; i < tt.headerCount; i++ {
				req.Header.Set("X-Test-"+strings.Repeat(string(rune('A'+i%26)), i+1), "value")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d (headerCount=%d, max=%d)",
					rec.Code, tt.wantStatus, tt.headerCount, tt.maxHeaderCount)
			}
			if tt.wantStatus == http.StatusBadRequest {
				body := inputValidationBody(t, rec)
				if body.Error != "input_validation_failed" {
					t.Errorf("expected error code 'input_validation_failed', got %q", body.Error)
				}
				if called {
					t.Error("next handler must not be called when validation fails")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Header value size
// ---------------------------------------------------------------------------

func TestInputValidation_HeaderSize(t *testing.T) {
	tests := []struct {
		name          string
		maxHeaderSize int
		headerValue   string
		wantStatus    int
	}{
		{
			name:          "within limit",
			maxHeaderSize: 8192,
			headerValue:   strings.Repeat("x", 100),
			wantStatus:    http.StatusOK,
		},
		{
			name:          "at limit",
			maxHeaderSize: 50,
			headerValue:   strings.Repeat("y", 50),
			wantStatus:    http.StatusOK,
		},
		{
			name:          "exceeds limit",
			maxHeaderSize: 50,
			headerValue:   strings.Repeat("y", 51),
			wantStatus:    http.StatusBadRequest,
		},
		{
			name:          "zero disables check",
			maxHeaderSize: 0,
			headerValue:   strings.Repeat("z", 100000),
			wantStatus:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := InputValidationConfig{
				Enabled:       true,
				MaxHeaderSize: tt.maxHeaderSize,
			}
			called := false
			handler := InputValidation(cfg)(noopHandlerFunc(&called))

			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.RequestURI = "/path"
			req.Header.Set("X-Big-Header", tt.headerValue)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusBadRequest {
				body := inputValidationBody(t, rec)
				if body.Error != "input_validation_failed" {
					t.Errorf("expected error code 'input_validation_failed', got %q", body.Error)
				}
				if called {
					t.Error("next handler must not be called when validation fails")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Path overrides
// ---------------------------------------------------------------------------

func TestInputValidation_PathOverrides(t *testing.T) {
	cfg := InputValidationConfig{
		Enabled:              true,
		MaxURLLength:         100,
		MaxQueryStringLength: 50,
		MaxHeaderCount:       10,
		MaxHeaderSize:        200,
		PathOverrides: []InputValidationPathOverride{
			{
				// /api/upload gets a relaxed query string limit.
				Path:                 "/api/upload",
				MaxQueryStringLength: 5000,
			},
			{
				// /strict/* gets a tighter URL limit.
				Path:         "/strict/*",
				MaxURLLength: 20,
			},
		},
	}

	handler := InputValidation(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("override relaxes query string limit on matching path", func(t *testing.T) {
		// Query is 60 chars — exceeds global limit (50) but under override (5000).
		// The URI is "/api/upload?" + 60 q's = 72 chars, which is under the global
		// MaxURLLength of 100, so only the query string check is relevant.
		longQuery := strings.Repeat("q", 60)
		req := httptest.NewRequest(http.MethodGet, "/api/upload?"+longQuery, nil)
		req.RequestURI = "/api/upload?" + longQuery
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 on overridden path, got %d", rec.Code)
		}
	})

	t.Run("global query string limit applies on non-matching path", func(t *testing.T) {
		longQuery := strings.Repeat("q", 51) // exceeds global 50
		req := httptest.NewRequest(http.MethodGet, "/other?"+longQuery, nil)
		req.RequestURI = "/other?" + longQuery
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 on non-matching path, got %d", rec.Code)
		}
	})

	t.Run("override tightens URL limit on wildcard path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/strict/foo", nil)
		req.RequestURI = "/strict/" + strings.Repeat("a", 20) // exceeds override 20
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 on strict path with long URI, got %d", rec.Code)
		}
	})

	t.Run("first matching override wins", func(t *testing.T) {
		// /api/upload matches the first override (MaxQueryStringLength=5000).
		// Query is 55 chars — exceeds global limit (50) but under override (5000).
		// URI "/api/upload?" + 55 q's = 67 chars, under global MaxURLLength of 100.
		mediumQuery := strings.Repeat("q", 55)
		req := httptest.NewRequest(http.MethodGet, "/api/upload?"+mediumQuery, nil)
		req.RequestURI = "/api/upload?" + mediumQuery
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 (first override wins), got %d", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Error response structure
// ---------------------------------------------------------------------------

func TestInputValidation_ErrorResponseStructure(t *testing.T) {
	cfg := InputValidationConfig{
		Enabled:      true,
		MaxURLLength: 5,
	}
	handler := InputValidation(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/toolong", nil)
	req.RequestURI = "/toolong"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	body := inputValidationBody(t, rec)
	if body.Error != "input_validation_failed" {
		t.Errorf("expected error code 'input_validation_failed', got %q", body.Error)
	}
	if body.Status != http.StatusBadRequest {
		t.Errorf("expected status 400 in body, got %d", body.Status)
	}
	if body.Message == "" {
		t.Error("expected non-empty message in error response")
	}
	// Must have a correlation ID (either trace_id or request_id).
	if body.TraceID == "" && body.RequestID == "" {
		t.Error("expected trace_id or request_id in error response")
	}
}

// ---------------------------------------------------------------------------
// All checks pass — next handler is called
// ---------------------------------------------------------------------------

func TestInputValidation_AllCheckPass(t *testing.T) {
	cfg := InputValidationConfig{
		Enabled:              true,
		MaxURLLength:         2048,
		MaxQueryStringLength: 2048,
		MaxHeaderCount:       100,
		MaxHeaderSize:        8192,
	}

	called := false
	handler := InputValidation(cfg)(noopHandlerFunc(&called))

	req := httptest.NewRequest(http.MethodPost, "/api/data?foo=bar", nil)
	req.RequestURI = "/api/data?foo=bar"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected next handler to be called when all checks pass")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Default limits
// ---------------------------------------------------------------------------

func TestInputValidation_DefaultLimits(t *testing.T) {
	// Build a config that matches the defaults described in the issue.
	cfg := InputValidationConfig{
		Enabled:              true,
		MaxURLLength:         2048,
		MaxQueryStringLength: 2048,
		MaxHeaderCount:       100,
		MaxHeaderSize:        8192,
	}

	tests := []struct {
		name       string
		setup      func(*http.Request)
		wantStatus int
	}{
		{
			name: "URL at default limit",
			setup: func(r *http.Request) {
				r.RequestURI = "/" + strings.Repeat("a", 2047) // 2048 total
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "URL exceeds default limit",
			setup: func(r *http.Request) {
				r.RequestURI = "/" + strings.Repeat("a", 2048) // 2049 total
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "query string at default limit",
			setup: func(r *http.Request) {
				// Set RawQuery to exactly the default limit (2048 chars).
				// Keep RequestURI short so the URL-length check (also 2048) does
				// not fire before the query-string check.
				r.URL.RawQuery = strings.Repeat("q", 2048)
				r.RequestURI = "/"
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "query string exceeds default limit",
			setup: func(r *http.Request) {
				// 2049 chars — one over the default limit.
				r.URL.RawQuery = strings.Repeat("q", 2049)
				r.RequestURI = "/"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "header count at default limit",
			setup: func(r *http.Request) {
				r.Header = make(http.Header)
				for i := 0; i < 100; i++ {
					r.Header.Set("X-H-"+strings.Repeat("A", i+1), "v")
				}
				r.RequestURI = "/"
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "header count exceeds default limit",
			setup: func(r *http.Request) {
				r.Header = make(http.Header)
				for i := 0; i < 101; i++ {
					r.Header.Set("X-H-"+strings.Repeat("A", i+1), "v")
				}
				r.RequestURI = "/"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "header value at default size limit",
			setup: func(r *http.Request) {
				r.RequestURI = "/"
				r.Header.Set("X-Big", strings.Repeat("v", 8192))
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "header value exceeds default size limit",
			setup: func(r *http.Request) {
				r.RequestURI = "/"
				r.Header.Set("X-Big", strings.Repeat("v", 8193))
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := InputValidation(cfg)(noopHandlerFunc(&called))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(req)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
