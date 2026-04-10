package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// handleWatchEvents
// ---------------------------------------------------------------------------

// eventsFixture is the JSON the mock sidecar returns for a healthy response.
const eventsFixture = `{
	"events": [
		{
			"cursor": 1,
			"event_type": "request",
			"timestamp": "2026-04-03T10:00:00Z",
			"ai_summary": "GET /api/v1/users returned 200",
			"payload": {"method": "GET", "path": "/api/v1/users", "status": 200}
		},
		{
			"cursor": 2,
			"event_type": "auth_failure",
			"timestamp": "2026-04-03T10:00:01Z",
			"ai_summary": "Authentication failed for POST /api/v1/secret",
			"payload": {"method": "POST", "path": "/api/v1/secret", "status": 401}
		}
	],
	"cursor": 2
}`

func TestHandleWatchEvents_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_vibewarden/admin/events" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(eventsFixture))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "test-token",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}

	var result watchEventsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result JSON: %v\nraw: %s", err, items[0].Text)
	}

	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
	if result.Cursor != 2 {
		t.Errorf("Cursor = %d, want 2", result.Cursor)
	}
	if len(result.Events) != 2 {
		t.Errorf("len(Events) = %d, want 2", len(result.Events))
	}
}

func TestHandleWatchEvents_SinceCursor(t *testing.T) {
	var capturedSince string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSince = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":5}`))
	}))
	defer srv.Close()

	since := uint64(3)
	params, _ := json.Marshal(map[string]any{
		"url":          srv.URL,
		"admin_token":  "tok",
		"since_cursor": since,
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedSince != "3" {
		t.Errorf("since query param = %q, want %q", capturedSince, "3")
	}

	var result watchEventsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result JSON: %v", err)
	}
	if result.Cursor != 5 {
		t.Errorf("Cursor = %d, want 5", result.Cursor)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestHandleWatchEvents_TypeFilter(t *testing.T) {
	var capturedType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedType = r.URL.Query().Get("type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"types":       "request,auth_failure",
	})

	_, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedType != "request,auth_failure" {
		t.Errorf("type query param = %q, want %q", capturedType, "request,auth_failure")
	}
}

func TestHandleWatchEvents_LimitParam(t *testing.T) {
	var capturedLimit string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	lim := 25
	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"limit":       lim,
	})

	_, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedLimit != "25" {
		t.Errorf("limit query param = %q, want %q", capturedLimit, "25")
	}
}

func TestHandleWatchEvents_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "wrong-token",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	// Response must mention authentication failure, not the token.
	if !strings.Contains(strings.ToLower(items[0].Text), "authentication failed") {
		t.Errorf("expected authentication failure message, got: %s", items[0].Text)
	}
	// The admin token must never appear in the response.
	if strings.Contains(items[0].Text, "wrong-token") {
		t.Error("admin token must not appear in the response")
	}
}

func TestHandleWatchEvents_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "forbidden-token",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "authentication failed") {
		t.Errorf("expected authentication failure message, got: %s", items[0].Text)
	}
	if strings.Contains(items[0].Text, "forbidden-token") {
		t.Error("admin token must not appear in the response")
	}
}

func TestHandleWatchEvents_ServiceUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "ring buffer") {
		t.Errorf("expected ring buffer unavailable message, got: %s", items[0].Text)
	}
}

func TestHandleWatchEvents_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(items[0].Text, "500") {
		t.Errorf("expected HTTP 500 in message, got: %s", items[0].Text)
	}
}

func TestHandleWatchEvents_ConnectionError(t *testing.T) {
	// Nothing is listening on this port.
	params, _ := json.Marshal(map[string]any{
		"url":         "http://127.0.0.1:19998",
		"admin_token": "secret-token",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}
	// Token must not leak in connection error message.
	if strings.Contains(items[0].Text, "secret-token") {
		t.Error("admin token must not appear in the connection error message")
	}
	// Message must be helpful.
	if !strings.Contains(strings.ToLower(items[0].Text), "sidecar") {
		t.Errorf("expected helpful sidecar message, got: %s", items[0].Text)
	}
}

func TestHandleWatchEvents_MissingURL(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"admin_token": "tok",
	})
	_, err := handleWatchEvents(context.Background(), params)
	if err == nil {
		t.Error("expected an error when url is missing")
	}
}

func TestHandleWatchEvents_MissingToken(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url": "http://localhost:8443",
	})
	_, err := handleWatchEvents(context.Background(), params)
	if err == nil {
		t.Error("expected an error when admin_token is missing")
	}
}

func TestHandleWatchEvents_InvalidArgs(t *testing.T) {
	_, err := handleWatchEvents(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleWatchEvents_EmptyEventsNotNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	items, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Ensure the serialised output contains an array, not null.
	if strings.Contains(items[0].Text, `"events": null`) {
		t.Error("events field must be an array, not null")
	}
	if !strings.Contains(items[0].Text, `"events": []`) {
		t.Errorf("expected empty array for events, got: %s", items[0].Text)
	}
}

// TestHandleWatchEvents_TrailingSlashURL ensures a trailing slash on the base URL
// does not result in a double-slash path.
func TestHandleWatchEvents_TrailingSlashURL(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL + "/",
		"admin_token": "tok",
	})

	_, err := handleWatchEvents(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/_vibewarden/admin/events" {
		t.Errorf("path = %q, want %q", capturedPath, "/_vibewarden/admin/events")
	}
}

// TestRegisterDefaultTools_IncludesWatchEvents ensures the tool is registered.
func TestRegisterDefaultTools_IncludesWatchEvents(t *testing.T) {
	srv := newTestServer()
	RegisterDefaultTools(srv)

	if _, ok := srv.handlers["vibewarden_watch_events"]; !ok {
		t.Error("vibewarden_watch_events not registered in RegisterDefaultTools")
	}
}
