package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/auth"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeAPIKeyValidator is a simple in-memory fake that implements
// ports.APIKeyValidator without any mocking framework.
type fakeAPIKeyValidator struct {
	// keys maps plaintext key → APIKey to return on a match.
	keys map[string]*auth.APIKey
	// err, when non-nil, is returned for every Validate call.
	err error
}

func (f *fakeAPIKeyValidator) Validate(_ context.Context, plaintextKey string) (*auth.APIKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	k, ok := f.keys[plaintextKey]
	if !ok {
		return nil, ports.ErrAPIKeyInvalid
	}
	return k, nil
}

// validAPIKey returns a well-formed, active APIKey for use in tests.
func validAPIKey() *auth.APIKey {
	return &auth.APIKey{
		Name:    "test-key",
		KeyHash: auth.HashKey("vw_test_secret"),
		Scopes:  []auth.Scope{"read:metrics"},
		Active:  true,
	}
}

func TestAPIKeyMiddleware_MissingHeader(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{Header: "X-API-Key"}

	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (401)", w.Code, http.StatusUnauthorized)
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Error != "unauthorized" {
		t.Errorf("error code = %q, want %q", body.Error, "unauthorized")
	}
}

func TestAPIKeyMiddleware_InvalidKey(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{Header: "X-API-Key"}

	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "bad-key")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (401)", w.Code, http.StatusUnauthorized)
	}
}

func TestAPIKeyMiddleware_ValidKey(t *testing.T) {
	apiKey := validAPIKey()
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_test_secret": apiKey,
		},
	}
	cfg := ports.APIKeyConfig{Header: "X-API-Key"}

	var nextCtx context.Context
	nextCalled := false
	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		nextCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !nextCalled {
		t.Fatal("next handler was not called for valid API key")
	}

	gotKey, ok := APIKeyFromContext(nextCtx)
	if !ok {
		t.Fatal("api key not stored in context")
	}
	if gotKey.Name != apiKey.Name {
		t.Errorf("context key name = %q, want %q", gotKey.Name, apiKey.Name)
	}
}

func TestAPIKeyMiddleware_CustomHeader(t *testing.T) {
	apiKey := validAPIKey()
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_test_secret": apiKey,
		},
	}
	cfg := ports.APIKeyConfig{Header: "Authorization"}

	nextCalled := false
	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	// Key in wrong header → should reject.
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if nextCalled {
		t.Error("next should not be called when key is in wrong header")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Key in correct header → should succeed.
	nextCalled = false
	req2 := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req2.Header.Set("Authorization", "vw_test_secret")
	w2 := httptest.NewRecorder()
	mw(next).ServeHTTP(w2, req2)

	if !nextCalled {
		t.Error("next should be called when key is in correct header")
	}
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestAPIKeyMiddleware_DefaultHeader(t *testing.T) {
	apiKey := validAPIKey()
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_test_secret": apiKey,
		},
	}
	// Empty header → must default to X-API-Key.
	cfg := ports.APIKeyConfig{}

	nextCalled := false
	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next should be called when default header is used")
	}
}

func TestAPIKeyMiddleware_401IsJSON(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}

	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	// No header at all.
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.RequestID == "" && body.TraceID == "" {
		t.Error("expected request_id or trace_id in 401 response body")
	}
}

func TestAPIKeyMiddleware_EmitsSuccessEvent(t *testing.T) {
	apiKey := validAPIKey()
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_test_secret": apiKey,
		},
	}
	cfg := ports.APIKeyConfig{}
	spy := &fakeEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, spy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_test_secret")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !spy.hasEventType(events.EventTypeAPIKeySuccess) {
		t.Error("expected auth.api_key.success event but none was logged")
	}
	ev := spy.logged[0]
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if ev.Payload["key_name"] != apiKey.Name {
		t.Errorf("payload.key_name = %v, want %q", ev.Payload["key_name"], apiKey.Name)
	}
}

func TestAPIKeyMiddleware_EmitsFailedEventOnMissingKey(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}
	spy := &fakeEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, spy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !spy.hasEventType(events.EventTypeAPIKeyFailed) {
		t.Error("expected auth.api_key.failed event but none was logged")
	}
	ev := spy.logged[0]
	if ev.Payload["reason"] != "missing api key" {
		t.Errorf("payload.reason = %v, want %q", ev.Payload["reason"], "missing api key")
	}
}

func TestAPIKeyMiddleware_EmitsFailedEventOnInvalidKey(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}
	spy := &fakeEventLogger{}

	mw := APIKeyMiddleware(validator, cfg, spy)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "bad-key")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if !spy.hasEventType(events.EventTypeAPIKeyFailed) {
		t.Error("expected auth.api_key.failed event but none was logged")
	}
	ev := spy.logged[0]
	if ev.Payload["reason"] != "invalid or inactive api key" {
		t.Errorf("payload.reason = %v, want %q", ev.Payload["reason"], "invalid or inactive api key")
	}
}

func TestAPIKeyMiddleware_NilEventLoggerDoesNotPanic(t *testing.T) {
	validator := &fakeAPIKeyValidator{keys: map[string]*auth.APIKey{}}
	cfg := ports.APIKeyConfig{}

	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	w := httptest.NewRecorder()

	// Must not panic with nil event logger.
	mw(next).ServeHTTP(w, req)
}

func TestAPIKeyFromContext_Empty(t *testing.T) {
	k, ok := APIKeyFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for empty context")
	}
	if k != nil {
		t.Error("expected nil key for empty context")
	}
}

func TestAPIKeyMiddleware_KeyScopesInContext(t *testing.T) {
	apiKey := &auth.APIKey{
		Name:    "scoped",
		KeyHash: auth.HashKey("vw_scoped_key"),
		Scopes:  []auth.Scope{"read:metrics", "write:config"},
		Active:  true,
	}
	validator := &fakeAPIKeyValidator{
		keys: map[string]*auth.APIKey{
			"vw_scoped_key": apiKey,
		},
	}
	cfg := ports.APIKeyConfig{}

	var gotKey *auth.APIKey
	mw := APIKeyMiddleware(validator, cfg, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, _ = APIKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-API-Key", "vw_scoped_key")
	w := httptest.NewRecorder()
	mw(next).ServeHTTP(w, req)

	if gotKey == nil {
		t.Fatal("expected key in context, got nil")
	}
	if len(gotKey.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(gotKey.Scopes))
	}
	if gotKey.Scopes[0] != "read:metrics" {
		t.Errorf("Scopes[0] = %q, want %q", gotKey.Scopes[0], "read:metrics")
	}
}
