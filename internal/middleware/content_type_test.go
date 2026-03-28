package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentTypeValidation_NoBodyMethod(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: true,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	noBodyMethods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodDelete,
		http.MethodOptions,
	}

	for _, method := range noBodyMethods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/resource", nil)
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("%s: expected next to be called, but it was not", method)
			}
			if w.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", method, w.Code, http.StatusOK)
			}
		})
	}
}

func TestContentTypeValidation_Disabled(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: false,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	// POST with wrong Content-Type must pass when disabled.
	req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Error("disabled: expected next to be called, but it was not")
	}
	if w.Code != http.StatusOK {
		t.Errorf("disabled: status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestContentTypeValidation_MissingContentType(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: true,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	bodyMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
	}

	for _, method := range bodyMethods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/resource", nil)
			// no Content-Type header set
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw(next).ServeHTTP(w, req)

			if nextCalled {
				t.Errorf("%s: next should not be called when Content-Type is missing", method)
			}
			if w.Code != http.StatusUnsupportedMediaType {
				t.Errorf("%s: status = %d, want %d", method, w.Code, http.StatusUnsupportedMediaType)
			}
		})
	}
}

func TestContentTypeValidation_AllowedContentType(t *testing.T) {
	tests := []struct {
		name        string
		allowed     []string
		contentType string
	}{
		{
			name:        "exact match application/json",
			allowed:     []string{"application/json"},
			contentType: "application/json",
		},
		{
			name:        "match with charset parameter",
			allowed:     []string{"application/json"},
			contentType: "application/json; charset=utf-8",
		},
		{
			name:        "multipart/form-data",
			allowed:     []string{"application/json", "multipart/form-data"},
			contentType: "multipart/form-data; boundary=something",
		},
		{
			name:        "application/x-www-form-urlencoded",
			allowed:     []string{"application/x-www-form-urlencoded"},
			contentType: "application/x-www-form-urlencoded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ContentTypeValidationConfig{
				Enabled: true,
				Allowed: tt.allowed,
			}
			mw := ContentTypeValidation(cfg)

			req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("%s: expected next to be called", tt.name)
			}
			if w.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", tt.name, w.Code, http.StatusOK)
			}
		})
	}
}

func TestContentTypeValidation_DisallowedContentType(t *testing.T) {
	tests := []struct {
		name        string
		allowed     []string
		contentType string
	}{
		{
			name:        "text/plain not in allowed list",
			allowed:     []string{"application/json"},
			contentType: "text/plain",
		},
		{
			name:        "text/xml not in allowed list",
			allowed:     []string{"application/json", "application/x-www-form-urlencoded"},
			contentType: "text/xml",
		},
		{
			name:        "application/xml not allowed when only json allowed",
			allowed:     []string{"application/json"},
			contentType: "application/xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ContentTypeValidationConfig{
				Enabled: true,
				Allowed: tt.allowed,
			}
			mw := ContentTypeValidation(cfg)

			req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw(next).ServeHTTP(w, req)

			if nextCalled {
				t.Errorf("%s: next should not be called for disallowed Content-Type", tt.name)
			}
			if w.Code != http.StatusUnsupportedMediaType {
				t.Errorf("%s: status = %d, want %d", tt.name, w.Code, http.StatusUnsupportedMediaType)
			}
		})
	}
}

func TestContentTypeValidation_AllBodyMethods(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: true,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	bodyMethods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
	}

	for _, method := range bodyMethods {
		t.Run(method+" allowed content type passes", func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/resource", nil)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			mw(next).ServeHTTP(w, req)

			if !nextCalled {
				t.Errorf("%s: expected next to be called", method)
			}
			if w.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", method, w.Code, http.StatusOK)
			}
		})
	}
}

func TestContentTypeValidation_ErrorResponseIsJSON(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: true,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
	// No Content-Type header.
	w := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestContentTypeValidation_InvalidContentTypeHeader(t *testing.T) {
	cfg := ContentTypeValidationConfig{
		Enabled: true,
		Allowed: []string{"application/json"},
	}
	mw := ContentTypeValidation(cfg)

	// A Content-Type that cannot be parsed by mime.ParseMediaType.
	req := httptest.NewRequest(http.MethodPost, "/api/resource", nil)
	req.Header.Set("Content-Type", "not/a/valid///content-type")
	w := httptest.NewRecorder()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw(next).ServeHTTP(w, req)

	if nextCalled {
		t.Error("next should not be called for an invalid Content-Type value")
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}
