package middleware_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/llm"
	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---- fakes ----

// fakeResponseValidationEventLogger records all logged events for assertion.
type fakeResponseValidationEventLogger struct {
	mu  sync.Mutex
	evs []events.Event
}

func (f *fakeResponseValidationEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evs = append(f.evs, ev)
	return nil
}

func (f *fakeResponseValidationEventLogger) EventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, len(f.evs))
	for i, ev := range f.evs {
		types[i] = ev.EventType
	}
	return types
}

func (f *fakeResponseValidationEventLogger) Snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]events.Event, len(f.evs))
	copy(cp, f.evs)
	return cp
}

// interface guard
var _ ports.EventLogger = (*fakeResponseValidationEventLogger)(nil)

// ---- helpers ----

// openAISchema builds a SchemaDefinition that requires choices[].message.
func openAISchema(t *testing.T) llm.SchemaDefinition {
	t.Helper()
	doc := map[string]any{
		"type":     "object",
		"required": []any{"choices"},
		"properties": map[string]any{
			"choices": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []any{"message"},
				},
			},
		},
	}
	sd, err := llm.NewSchemaDefinition(doc)
	if err != nil {
		t.Fatalf("openAISchema: %v", err)
	}
	return sd
}

// buildValidator wraps openAISchema in a ResponseValidator.
func buildValidator(t *testing.T) llm.ResponseValidator {
	t.Helper()
	sd := openAISchema(t)
	v, err := llm.NewResponseValidator(sd)
	if err != nil {
		t.Fatalf("buildValidator: %v", err)
	}
	return v
}

// upstreamHandler creates an httptest.Server that writes the given status, ct,
// and body, and returns an HTTP middleware handler (the upstream handler).
func upstreamHandler(status int, ct, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newMiddlewareRoutes builds a single-route slice from the given parameters.
func newMiddlewareRoutes(name, pattern string, v llm.ResponseValidator, action middleware.LLMResponseValidationAction) []middleware.LLMResponseValidationRouteConfig {
	return []middleware.LLMResponseValidationRouteConfig{
		{
			RouteName:    name,
			RoutePattern: pattern,
			Enabled:      true,
			Validator:    v,
			Action:       action,
		},
	}
}

// invokeMiddleware runs the middleware against an upstream handler and returns
// the recorded response.
func invokeMiddleware(
	t *testing.T,
	upstream http.Handler,
	routes []middleware.LLMResponseValidationRouteConfig,
	targetURL string,
	eventLogger ports.EventLogger,
) *httptest.ResponseRecorder {
	t.Helper()
	mw := middleware.LLMResponseValidationMiddleware(routes, slog.Default(), eventLogger)
	handler := mw(upstream)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"prompt":"hello"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Egress-URL", targetURL)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// ---- tests ----

// TestLLMResponseValidation_NoRoutes verifies that the middleware is a no-op
// when the route list is empty.
func TestLLMResponseValidation_NoRoutes(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json",
		`{"model":"gpt-4"}`) // would fail schema

	mw := middleware.LLMResponseValidationMiddleware(nil, slog.Default(), nil)
	handler := mw(upstream)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Egress-URL", "https://api.openai.com/v1/chat/completions")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 (no routes should pass through)", rr.Code)
	}
}

// TestLLMResponseValidation_NoMatchingRoute verifies that requests not matching
// any configured route are passed through unchanged.
func TestLLMResponseValidation_NoMatchingRoute(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json", `{"model":"gpt-4"}`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	// Use a URL that does NOT match the pattern.
	rr := invokeMiddleware(t, upstream, routes, "https://api.anthropic.com/v1/messages", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 (non-matching route should pass through)", rr.Code)
	}
}

// TestLLMResponseValidation_Block_ValidResponse verifies that a valid JSON
// response passes through when action=block.
func TestLLMResponseValidation_Block_ValidResponse(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json",
		`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes, "https://api.openai.com/v1/chat/completions", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for valid response", rr.Code)
	}
}

// TestLLMResponseValidation_Block_InvalidResponse verifies that an invalid JSON
// response is blocked (502) when action=block.
func TestLLMResponseValidation_Block_InvalidResponse(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json",
		`{"model":"gpt-4"}`) // missing "choices"

	el := &fakeResponseValidationEventLogger{}
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502 for blocked invalid response", rr.Code)
	}

	// An llm.response_invalid event must have been emitted.
	evTypes := el.EventTypes()
	found := false
	for _, et := range evTypes {
		if et == events.EventTypeLLMResponseInvalid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected llm.response_invalid event, got types: %v", evTypes)
	}
}

// TestLLMResponseValidation_Warn_InvalidResponse verifies that an invalid
// response is passed through (200) when action=warn.
func TestLLMResponseValidation_Warn_InvalidResponse(t *testing.T) {
	const body = `{"model":"gpt-4"}` // missing "choices"
	upstream := upstreamHandler(http.StatusOK, "application/json", body)

	el := &fakeResponseValidationEventLogger{}
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionWarn)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for warn mode pass-through", rr.Code)
	}
	if rr.Body.String() != body {
		t.Errorf("body = %q, want %q", rr.Body.String(), body)
	}

	// An llm.response_invalid event must still be emitted.
	evTypes := el.EventTypes()
	found := false
	for _, et := range evTypes {
		if et == events.EventTypeLLMResponseInvalid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected llm.response_invalid event in warn mode, got types: %v", evTypes)
	}
}

// TestLLMResponseValidation_NonJSON_PassThrough verifies that non-JSON responses
// are never validated and always pass through.
func TestLLMResponseValidation_NonJSON_PassThrough(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "text/plain", `some plain text`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	el := &fakeResponseValidationEventLogger{}
	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	// Non-JSON must pass through regardless.
	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for non-JSON pass-through", rr.Code)
	}
	if len(el.EventTypes()) > 0 {
		t.Errorf("expected no events for non-JSON response, got: %v", el.EventTypes())
	}
}

// TestLLMResponseValidation_Block_InvalidJSON_Body verifies that a malformed
// JSON response body is blocked when action=block.
func TestLLMResponseValidation_Block_InvalidJSON_Body(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json", `not json`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	el := &fakeResponseValidationEventLogger{}
	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502 for malformed JSON body", rr.Code)
	}
	evTypes := el.EventTypes()
	found := false
	for _, et := range evTypes {
		if et == events.EventTypeLLMResponseInvalid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected llm.response_invalid event for malformed JSON, got: %v", evTypes)
	}
}

// TestLLMResponseValidation_Warn_InvalidJSON_Body verifies that a malformed
// JSON response body is passed through (with event) when action=warn.
func TestLLMResponseValidation_Warn_InvalidJSON_Body(t *testing.T) {
	const body = `not json`
	upstream := upstreamHandler(http.StatusOK, "application/json", body)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionWarn)

	el := &fakeResponseValidationEventLogger{}
	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for warn mode pass-through", rr.Code)
	}
	if rr.Body.String() != body {
		t.Errorf("body = %q, want %q", rr.Body.String(), body)
	}
	if len(el.EventTypes()) == 0 {
		t.Error("expected llm.response_invalid event for warn mode malformed JSON")
	}
}

// TestLLMResponseValidation_ResponseBodyPreserved verifies that on a passing
// validation the response body is forwarded intact.
func TestLLMResponseValidation_ResponseBodyPreserved(t *testing.T) {
	const validBody = `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"id":"abc"}`
	upstream := upstreamHandler(http.StatusOK, "application/json", validBody)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", rr.Code)
	}
	if rr.Body.String() != validBody {
		t.Errorf("body = %q, want %q", rr.Body.String(), validBody)
	}
}

// TestLLMResponseValidation_EventPayload verifies that the emitted event
// contains the expected fields.
func TestLLMResponseValidation_EventPayload(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json", `{"model":"gpt-4"}`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	el := &fakeResponseValidationEventLogger{}
	invokeMiddleware(t, upstream, routes, "https://api.openai.com/v1/chat/completions", el)

	evs := el.Snapshot()
	var invalidEv *events.Event
	for i := range evs {
		if evs[i].EventType == events.EventTypeLLMResponseInvalid {
			invalidEv = &evs[i]
			break
		}
	}
	if invalidEv == nil {
		t.Fatal("no llm.response_invalid event emitted")
	}

	payload := invalidEv.Payload
	for _, key := range []string{"route", "method", "url", "action", "violations"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("event payload missing key %q", key)
		}
	}
	if route, ok := payload["route"].(string); !ok || route != "openai" {
		t.Errorf("payload[route] = %v, want \"openai\"", payload["route"])
	}
	if action, ok := payload["action"].(string); !ok || action != "block" {
		t.Errorf("payload[action] = %v, want \"block\"", payload["action"])
	}
}

// TestLLMResponseValidation_DisabledRoute verifies that a route with
// Enabled=false is not evaluated.
func TestLLMResponseValidation_DisabledRoute(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json", `{"model":"gpt-4"}`)
	// Enabled=false — route should be skipped.
	routes := []middleware.LLMResponseValidationRouteConfig{
		{
			RouteName:    "openai",
			RoutePattern: "https://api.openai.com/**",
			Enabled:      false,
			Validator:    buildValidator(t),
			Action:       middleware.LLMResponseValidationActionBlock,
		},
	}

	el := &fakeResponseValidationEventLogger{}
	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	// Disabled route — pass through without validation.
	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for disabled route", rr.Code)
	}
	if len(el.EventTypes()) > 0 {
		t.Errorf("expected no events for disabled route, got: %v", el.EventTypes())
	}
}

// TestLLMResponseValidation_NoTargetURL verifies that requests with no target
// URL (no X-Egress-URL header and no /_egress/ prefix) are passed through.
func TestLLMResponseValidation_NoTargetURL(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json", `{"model":"gpt-4"}`)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	mw := middleware.LLMResponseValidationMiddleware(routes, slog.Default(), nil)
	handler := mw(upstream)

	// Request without X-Egress-URL and not using /_egress/ path.
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 for request with no target URL", rr.Code)
	}
}

// TestLLMResponseValidation_ResponseHeadersPreserved verifies that upstream
// response headers are forwarded to the caller.
func TestLLMResponseValidation_ResponseHeadersPreserved(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-abc-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	})
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("X-Request-Id"); got != "req-abc-123" {
		t.Errorf("X-Request-Id = %q, want %q", got, "req-abc-123")
	}
}

// TestBuildLLMResponseValidationRoutes_Disabled verifies that disabled routes
// are omitted from the output.
func TestBuildLLMResponseValidationRoutes_Disabled(t *testing.T) {
	inputs := []middleware.LLMResponseValidationRouteInput{
		{Name: "r1", Pattern: "https://api.openai.com/**", Enabled: false,
			Schema: map[string]any{"type": "object"}, Action: "block"},
		{Name: "r2", Pattern: "https://api.anthropic.com/**", Enabled: true,
			Schema: map[string]any{"type": "object"}, Action: "warn"},
	}
	routes, err := middleware.BuildLLMResponseValidationRoutes(inputs)
	if err != nil {
		t.Fatalf("BuildLLMResponseValidationRoutes: unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("len(routes) = %d, want 1 (disabled route should be omitted)", len(routes))
	}
	if routes[0].RouteName != "r2" {
		t.Errorf("routes[0].RouteName = %q, want \"r2\"", routes[0].RouteName)
	}
}

// TestBuildLLMResponseValidationRoutes_BadSchema verifies that an invalid schema
// document returns an error.
func TestBuildLLMResponseValidationRoutes_BadSchema(t *testing.T) {
	inputs := []middleware.LLMResponseValidationRouteInput{
		{Name: "bad", Pattern: "https://api.openai.com/**", Enabled: true,
			Schema: nil, Action: "block"},
	}
	_, err := middleware.BuildLLMResponseValidationRoutes(inputs)
	if err == nil {
		t.Error("expected error for nil schema, got nil")
	}
}

// TestBuildLLMResponseValidationRoutes_DefaultAction verifies that an empty
// action defaults to "block".
func TestBuildLLMResponseValidationRoutes_DefaultAction(t *testing.T) {
	inputs := []middleware.LLMResponseValidationRouteInput{
		{Name: "r1", Pattern: "https://api.openai.com/**", Enabled: true,
			Schema: map[string]any{"type": "object"}, Action: ""},
	}
	routes, err := middleware.BuildLLMResponseValidationRoutes(inputs)
	if err != nil {
		t.Fatalf("BuildLLMResponseValidationRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if routes[0].Action != middleware.LLMResponseValidationActionBlock {
		t.Errorf("routes[0].Action = %q, want %q", routes[0].Action, middleware.LLMResponseValidationActionBlock)
	}
}

// TestLLMResponseValidation_ContentTypeWithCharset verifies that JSON content
// types with charset parameters are validated.
func TestLLMResponseValidation_ContentTypeWithCharset(t *testing.T) {
	upstream := upstreamHandler(http.StatusOK, "application/json; charset=utf-8",
		`{"model":"gpt-4"}`) // invalid — missing choices

	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	el := &fakeResponseValidationEventLogger{}
	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", el)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502 for invalid response with charset content-type", rr.Code)
	}
}

// TestLLMResponseValidation_UpstreamNon200_StillValidated verifies that
// non-200 upstream responses are still validated when JSON.
func TestLLMResponseValidation_UpstreamNon200_StillValidated(t *testing.T) {
	// 500 response with JSON body that does not match the schema.
	upstream := upstreamHandler(http.StatusInternalServerError, "application/json",
		`{"error":"internal server error"}`) // missing choices

	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", nil)

	// Blocked because schema validation failed (no choices field).
	if rr.Code != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502 (non-200 JSON should still be validated)", rr.Code)
	}
}

// TestLLMResponseValidation_ResponseBodyReadable verifies that the response body
// can be fully read by the caller after passing validation.
func TestLLMResponseValidation_ResponseBodyReadable(t *testing.T) {
	const validBody = `{"choices":[{"message":{"role":"assistant","content":"test"}}]}`
	upstream := upstreamHandler(http.StatusOK, "application/json", validBody)
	routes := newMiddlewareRoutes("openai", "https://api.openai.com/**",
		buildValidator(t), middleware.LLMResponseValidationActionBlock)

	rr := invokeMiddleware(t, upstream, routes,
		"https://api.openai.com/v1/chat/completions", nil)

	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	if string(body) != validBody {
		t.Errorf("body = %q, want %q", string(body), validBody)
	}
}
