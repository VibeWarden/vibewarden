package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamLogsFixture returns a JSON payload with events at various severity
// levels and event types for use in filter tests.
func streamLogsFixture(now time.Time) string {
	// Two minutes ago — should be within a 5m window but outside a 1m window.
	twoMinsAgo := now.Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	// Thirty seconds ago — should be inside a 1m window.
	thirtySecsAgo := now.Add(-30 * time.Second).UTC().Format(time.RFC3339)

	return fmt.Sprintf(`{
  "events": [
    {
      "event_type": "auth.failed",
      "timestamp": %q,
      "severity": "high",
      "ai_summary": "Authentication failed for 192.0.2.1"
    },
    {
      "event_type": "auth.success",
      "timestamp": %q,
      "severity": "info",
      "ai_summary": "User alice authenticated successfully"
    },
    {
      "event_type": "rate_limit.hit",
      "timestamp": %q,
      "severity": "medium",
      "ai_summary": "Rate limit exceeded for 198.51.100.2"
    },
    {
      "event_type": "request.blocked",
      "timestamp": %q,
      "severity": "critical",
      "ai_summary": "Request blocked by WAF: SQL injection pattern"
    }
  ],
  "cursor": 4
}`, twoMinsAgo, thirtySecsAgo, twoMinsAgo, thirtySecsAgo)
}

func newStreamLogsServer(t *testing.T, body func() string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = w.Write([]byte(body()))
	}))
}

func TestHandleStreamLogs_NoFilter(t *testing.T) {
	now := time.Now()
	srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "test-token",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one content item")
	}

	var result streamLogsResult
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result: %v\nraw: %s", err, items[0].Text)
	}
	// All 4 events should be returned when no filter is applied.
	if result.Count != 4 {
		t.Errorf("Count = %d, want 4", result.Count)
	}
}

func TestHandleStreamLogs_EventTypePrefix(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantCount int
	}{
		{"auth prefix matches two events", "auth", 2},
		{"rate_limit prefix matches one event", "rate_limit", 1},
		{"auth.failed exact match", "auth.failed", 1},
		{"no match", "tls", 0},
	}

	now := time.Now()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
			defer srv.Close()

			params, _ := json.Marshal(map[string]any{
				"url":         srv.URL,
				"admin_token": "test-token",
				"event_type":  tt.prefix,
			})

			items, err := handleStreamLogs(context.Background(), params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var result streamLogsResult
			if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
				t.Fatalf("cannot unmarshal result: %v", err)
			}
			if result.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", result.Count, tt.wantCount)
			}
			if result.Filters.EventTypePrefix != tt.prefix {
				t.Errorf("Filters.EventTypePrefix = %q, want %q", result.Filters.EventTypePrefix, tt.prefix)
			}
		})
	}
}

func TestHandleStreamLogs_Severity(t *testing.T) {
	tests := []struct {
		name      string
		severity  string
		wantCount int
	}{
		// info is not in the rank map, so min_severity=low should return low/medium/high/critical events
		{"low returns low+medium+high+critical (excludes info)", "low", 3},
		{"medium returns medium+high+critical events", "medium", 3},
		{"high returns high+critical", "high", 2},
		{"critical returns only critical", "critical", 1},
	}

	now := time.Now()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
			defer srv.Close()

			params, _ := json.Marshal(map[string]any{
				"url":         srv.URL,
				"admin_token": "test-token",
				"severity":    tt.severity,
			})

			items, err := handleStreamLogs(context.Background(), params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var result streamLogsResult
			if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
				t.Fatalf("cannot unmarshal result: %v", err)
			}
			if result.Count != tt.wantCount {
				t.Errorf("severity=%q: Count = %d, want %d", tt.severity, result.Count, tt.wantCount)
			}
			if result.Filters.MinSeverity != tt.severity {
				t.Errorf("Filters.MinSeverity = %q, want %q", result.Filters.MinSeverity, tt.severity)
			}
		})
	}
}

func TestHandleStreamLogs_Since(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		since     string
		wantCount int
	}{
		// 2 events are 30s ago (inside 1m), 2 events are 2m ago (outside 1m).
		{"1m window returns 2 recent events", "1m", 2},
		// All 4 events are within 5m.
		{"5m window returns all events", "5m", 4},
		// All events are older than 10s.
		{"10s window returns 0 events", "10s", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
			defer srv.Close()

			params, _ := json.Marshal(map[string]any{
				"url":         srv.URL,
				"admin_token": "test-token",
				"since":       tt.since,
			})

			items, err := handleStreamLogs(context.Background(), params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var result streamLogsResult
			if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
				t.Fatalf("cannot unmarshal result: %v", err)
			}
			if result.Count != tt.wantCount {
				t.Errorf("since=%q: Count = %d, want %d", tt.since, result.Count, tt.wantCount)
			}
			if result.Filters.Since != tt.since {
				t.Errorf("Filters.Since = %q, want %q", result.Filters.Since, tt.since)
			}
		})
	}
}

func TestHandleStreamLogs_CombinedFilters(t *testing.T) {
	now := time.Now()
	srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
	defer srv.Close()

	// event_type=auth, severity=high, since=5m
	// auth.failed: high, 2m ago -> passes all three
	// auth.success: info (not in rank map, fails severity filter)
	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "test-token",
		"event_type":  "auth",
		"severity":    "high",
		"since":       "5m",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result streamLogsResult
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("Count = %d, want 1", result.Count)
	}
}

func TestHandleStreamLogs_DefaultLimit(t *testing.T) {
	var capturedLimit string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	_, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedLimit != "50" {
		t.Errorf("limit query param = %q, want %q", capturedLimit, "50")
	}
}

func TestHandleStreamLogs_CustomLimit(t *testing.T) {
	var capturedLimit string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"cursor":0}`))
	}))
	defer srv.Close()

	lim := 100
	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"limit":       lim,
	})

	_, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedLimit != "100" {
		t.Errorf("limit query param = %q, want %q", capturedLimit, "100")
	}
}

func TestHandleStreamLogs_EmptyEventsNotNull(t *testing.T) {
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

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(items[0].Text, `"events": null`) {
		t.Error("events field must be an array, not null")
	}
	if !strings.Contains(items[0].Text, `"events": []`) {
		t.Errorf("expected empty array for events, got: %s", items[0].Text)
	}
}

func TestHandleStreamLogs_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "bad-token",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "authentication failed") {
		t.Errorf("expected authentication failure message, got: %s", items[0].Text)
	}
	if strings.Contains(items[0].Text, "bad-token") {
		t.Error("admin token must not appear in the response")
	}
}

func TestHandleStreamLogs_ServiceUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "ring buffer") {
		t.Errorf("expected ring buffer message, got: %s", items[0].Text)
	}
}

func TestHandleStreamLogs_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(items[0].Text, "500") {
		t.Errorf("expected HTTP 500 in message, got: %s", items[0].Text)
	}
}

func TestHandleStreamLogs_ConnectionError(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url":         "http://127.0.0.1:19997",
		"admin_token": "secret-token",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(items[0].Text, "secret-token") {
		t.Error("admin token must not appear in the connection error message")
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "sidecar") {
		t.Errorf("expected helpful sidecar message, got: %s", items[0].Text)
	}
}

func TestHandleStreamLogs_MissingURL(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"admin_token": "tok",
	})
	_, err := handleStreamLogs(context.Background(), params)
	if err == nil {
		t.Error("expected an error when url is missing")
	}
}

func TestHandleStreamLogs_MissingToken(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url": "http://localhost:8443",
	})
	_, err := handleStreamLogs(context.Background(), params)
	if err == nil {
		t.Error("expected an error when admin_token is missing")
	}
}

func TestHandleStreamLogs_InvalidArgs(t *testing.T) {
	_, err := handleStreamLogs(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected an error for invalid JSON arguments")
	}
}

func TestHandleStreamLogs_InvalidSeverity(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url":         "http://localhost:8443",
		"admin_token": "tok",
		"severity":    "ultra",
	})
	_, err := handleStreamLogs(context.Background(), params)
	if err == nil {
		t.Error("expected an error for invalid severity")
	}
}

func TestHandleStreamLogs_InvalidSince(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url":         "http://localhost:8443",
		"admin_token": "tok",
		"since":       "not-a-duration",
	})
	_, err := handleStreamLogs(context.Background(), params)
	if err == nil {
		t.Error("expected an error for invalid since duration")
	}
}

func TestHandleStreamLogs_TrailingSlashURL(t *testing.T) {
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

	_, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/_vibewarden/admin/events" {
		t.Errorf("path = %q, want %q", capturedPath, "/_vibewarden/admin/events")
	}
}

func TestHandleStreamLogs_SeverityCaseInsensitive(t *testing.T) {
	now := time.Now()
	srv := newStreamLogsServer(t, func() string { return streamLogsFixture(now) })
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "test-token",
		"severity":    "HIGH",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result streamLogsResult
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result: %v", err)
	}
	// high and critical events: auth.failed (high, 2m ago) + request.blocked (critical, 30s ago) = 2
	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
}

// TestRegisterDefaultTools_IncludesStreamLogs ensures the tool is registered.
func TestRegisterDefaultTools_IncludesStreamLogs(t *testing.T) {
	srv := newTestServer()
	RegisterDefaultTools(srv)

	if _, ok := srv.handlers["vibewarden_stream_logs"]; !ok {
		t.Error("vibewarden_stream_logs not registered in RegisterDefaultTools")
	}
}
