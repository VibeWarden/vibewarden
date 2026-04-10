package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// filterEvents unit tests
// ---------------------------------------------------------------------------

func TestFilterEvents_NoFilters(t *testing.T) {
	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "auth.success", "ai_summary": "auth ok", "timestamp": "2026-04-03T10:00:00Z"},
		{"event_type": "waf.detected", "ai_summary": "waf hit", "timestamp": "2026-04-03T10:00:01Z"},
	})

	got := filterEvents(raw, "", "", nil, 50)
	if len(got) != 2 {
		t.Errorf("expected 2 events, got %d", len(got))
	}
}

func TestFilterEvents_EventTypePrefixExact(t *testing.T) {
	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "auth.success", "ai_summary": "ok"},
		{"event_type": "waf.detected", "ai_summary": "waf"},
	})

	got := filterEvents(raw, "auth", "", nil, 50)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].EventType != "auth.success" {
		t.Errorf("EventType = %q, want %q", got[0].EventType, "auth.success")
	}
}

func TestFilterEvents_EventTypePrefixMultiple(t *testing.T) {
	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "waf.detected", "ai_summary": "d"},
		{"event_type": "waf.blocked", "ai_summary": "b"},
		{"event_type": "auth.success", "ai_summary": "a"},
		{"event_type": "waf.allowed", "ai_summary": "al"},
	})

	got := filterEvents(raw, "waf", "", nil, 50)
	if len(got) != 3 {
		t.Errorf("expected 3 waf.* events, got %d", len(got))
	}
	for _, e := range got {
		if !strings.HasPrefix(e.EventType, "waf.") {
			t.Errorf("unexpected event type %q", e.EventType)
		}
	}
}

func TestFilterEvents_EventTypeExactMatchWithoutDot(t *testing.T) {
	// An event_type of exactly "waf" (no dot) should also match "waf" prefix filter.
	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "waf", "ai_summary": "exact"},
		{"event_type": "waf.blocked", "ai_summary": "sub"},
	})

	got := filterEvents(raw, "waf", "", nil, 50)
	if len(got) != 2 {
		t.Errorf("expected 2 events, got %d", len(got))
	}
}

func TestFilterEvents_SeverityMinimum(t *testing.T) {
	tests := []struct {
		name        string
		minSeverity string
		events      []map[string]any
		wantCount   int
	}{
		{
			name:        "low includes all",
			minSeverity: "low",
			events: []map[string]any{
				{"event_type": "a", "severity": "low"},
				{"event_type": "b", "severity": "medium"},
				{"event_type": "c", "severity": "high"},
				{"event_type": "d", "severity": "critical"},
			},
			wantCount: 4,
		},
		{
			name:        "medium excludes low",
			minSeverity: "medium",
			events: []map[string]any{
				{"event_type": "a", "severity": "low"},
				{"event_type": "b", "severity": "medium"},
				{"event_type": "c", "severity": "high"},
			},
			wantCount: 2,
		},
		{
			name:        "high excludes low and medium",
			minSeverity: "high",
			events: []map[string]any{
				{"event_type": "a", "severity": "low"},
				{"event_type": "b", "severity": "medium"},
				{"event_type": "c", "severity": "high"},
				{"event_type": "d", "severity": "critical"},
			},
			wantCount: 2,
		},
		{
			name:        "critical only",
			minSeverity: "critical",
			events: []map[string]any{
				{"event_type": "a", "severity": "high"},
				{"event_type": "b", "severity": "critical"},
			},
			wantCount: 1,
		},
		{
			name:        "events without severity field excluded when filter active",
			minSeverity: "high",
			events: []map[string]any{
				{"event_type": "a"}, // no severity
				{"event_type": "b", "severity": "critical"},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := mustMarshalEvents(tt.events)
			got := filterEvents(raw, "", tt.minSeverity, nil, 50)
			if len(got) != tt.wantCount {
				t.Errorf("got %d events, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestFilterEvents_SinceFilter(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute)
	recent := now.Add(-1 * time.Minute)

	sinceThreshold := now.Add(-5 * time.Minute)

	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "a", "timestamp": old.Format(time.RFC3339)},
		{"event_type": "b", "timestamp": recent.Format(time.RFC3339)},
	})

	got := filterEvents(raw, "", "", &sinceThreshold, 50)
	if len(got) != 1 {
		t.Fatalf("expected 1 event after since filter, got %d", len(got))
	}
	if got[0].EventType != "b" {
		t.Errorf("expected event type %q, got %q", "b", got[0].EventType)
	}
}

func TestFilterEvents_SinceFilterBadTimestamp(t *testing.T) {
	// Events with unparseable timestamps must not be filtered out by since.
	sinceThreshold := time.Now().UTC().Add(-5 * time.Minute)

	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "a", "timestamp": "not-a-timestamp"},
	})

	// Event with bad timestamp passes through (we cannot verify age, so include it).
	got := filterEvents(raw, "", "", &sinceThreshold, 50)
	if len(got) != 1 {
		t.Errorf("expected 1 event (bad timestamp should pass through), got %d", len(got))
	}
}

func TestFilterEvents_LimitRespected(t *testing.T) {
	events := make([]map[string]any, 10)
	for i := range events {
		events[i] = map[string]any{"event_type": "x", "ai_summary": "s"}
	}
	raw := mustMarshalEvents(events)

	got := filterEvents(raw, "", "", nil, 3)
	if len(got) != 3 {
		t.Errorf("expected limit 3, got %d", len(got))
	}
}

func TestFilterEvents_EmptySliceNotNull(t *testing.T) {
	got := filterEvents(nil, "waf", "", nil, 50)
	if got == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 events, got %d", len(got))
	}
}

func TestFilterEvents_MalformedEventSkipped(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{invalid json`),
		json.RawMessage(`{"event_type":"good","ai_summary":"ok"}`),
	}
	got := filterEvents(raw, "", "", nil, 50)
	if len(got) != 1 {
		t.Errorf("expected 1 good event, got %d", len(got))
	}
}

func TestFilterEvents_AISummaryHoisted(t *testing.T) {
	raw := mustMarshalEvents([]map[string]any{
		{"event_type": "auth.success", "ai_summary": "User logged in successfully", "outcome": "allowed"},
	})

	got := filterEvents(raw, "", "", nil, 50)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].AISummary != "User logged in successfully" {
		t.Errorf("AISummary = %q, want %q", got[0].AISummary, "User logged in successfully")
	}
	if got[0].Outcome != "allowed" {
		t.Errorf("Outcome = %q, want %q", got[0].Outcome, "allowed")
	}
}

func TestFilterEvents_CombinedFilters(t *testing.T) {
	now := time.Now().UTC()
	sinceThreshold := now.Add(-5 * time.Minute)

	raw := mustMarshalEvents([]map[string]any{
		// passes: waf prefix, high severity, recent
		{"event_type": "waf.blocked", "severity": "high", "timestamp": now.Add(-1 * time.Minute).Format(time.RFC3339)},
		// fails event_type
		{"event_type": "auth.success", "severity": "high", "timestamp": now.Add(-1 * time.Minute).Format(time.RFC3339)},
		// fails severity
		{"event_type": "waf.detected", "severity": "low", "timestamp": now.Add(-1 * time.Minute).Format(time.RFC3339)},
		// fails since
		{"event_type": "waf.detected", "severity": "high", "timestamp": now.Add(-10 * time.Minute).Format(time.RFC3339)},
	})

	got := filterEvents(raw, "waf", "high", &sinceThreshold, 50)
	if len(got) != 1 {
		t.Fatalf("expected 1 event after combined filters, got %d", len(got))
	}
	if got[0].EventType != "waf.blocked" {
		t.Errorf("unexpected event type %q", got[0].EventType)
	}
}

// ---------------------------------------------------------------------------
// handleStreamLogs HTTP integration tests
// ---------------------------------------------------------------------------

const streamLogsFixture = `{
	"events": [
		{
			"event_type": "waf.blocked",
			"timestamp": "2026-04-03T10:00:00Z",
			"ai_summary": "WAF blocked SQL injection from 1.2.3.4",
			"severity": "high",
			"outcome": "blocked"
		},
		{
			"event_type": "auth.success",
			"timestamp": "2026-04-03T10:00:01Z",
			"ai_summary": "User logged in",
			"severity": "low",
			"outcome": "allowed"
		}
	],
	"cursor": 2
}`

func TestHandleStreamLogs_Success(t *testing.T) {
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
		_, _ = w.Write([]byte(streamLogsFixture))
	}))
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

	var result streamLogsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal result JSON: %v\nraw: %s", err, items[0].Text)
	}

	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
}

func TestHandleStreamLogs_EventTypeFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(streamLogsFixture))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"event_type":  "waf",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result streamLogsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("Count = %d, want 1 (only waf.* events)", result.Count)
	}
	if result.Events[0].EventType != "waf.blocked" {
		t.Errorf("EventType = %q, want %q", result.Events[0].EventType, "waf.blocked")
	}
	if result.Filters.EventType != "waf" {
		t.Errorf("Filters.EventType = %q, want %q", result.Filters.EventType, "waf")
	}
}

func TestHandleStreamLogs_SeverityFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(streamLogsFixture))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"severity":    "high",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result streamLogsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("Count = %d, want 1 (only high+ events)", result.Count)
	}
}

func TestHandleStreamLogs_LimitParam(t *testing.T) {
	// Build a fixture with 10 events.
	events := make([]map[string]any, 10)
	for i := range events {
		events[i] = map[string]any{
			"event_type": "request",
			"ai_summary": "ok",
			"timestamp":  "2026-04-03T10:00:00Z",
		}
	}
	fixture := map[string]any{"events": events, "cursor": 10}
	fixtureBytes, _ := json.Marshal(fixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixtureBytes)
	}))
	defer srv.Close()

	lim := 3
	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"limit":       lim,
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result streamLogsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal: %v", err)
	}
	if result.Count != 3 {
		t.Errorf("Count = %d, want 3", result.Count)
	}
	if result.Filters.Limit != 3 {
		t.Errorf("Filters.Limit = %d, want 3", result.Filters.Limit)
	}
}

func TestHandleStreamLogs_SinceFilter(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-1 * time.Minute).Format(time.RFC3339)
	old := now.Add(-10 * time.Minute).Format(time.RFC3339)

	fixture := map[string]any{
		"events": []map[string]any{
			{"event_type": "a", "ai_summary": "old", "timestamp": old},
			{"event_type": "b", "ai_summary": "recent", "timestamp": recent},
		},
		"cursor": 2,
	}
	fixtureBytes, _ := json.Marshal(fixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixtureBytes)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"since":       "5m",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result streamLogsResponse
	if err := json.Unmarshal([]byte(items[0].Text), &result); err != nil {
		t.Fatalf("cannot unmarshal: %v", err)
	}
	if result.Count != 1 {
		t.Errorf("Count = %d, want 1 (only recent event within 5m)", result.Count)
	}
	if result.Filters.Since != "5m" {
		t.Errorf("Filters.Since = %q, want %q", result.Filters.Since, "5m")
	}
}

func TestHandleStreamLogs_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]any{
		"url":         srv.URL,
		"admin_token": "wrong",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(items[0].Text), "authentication failed") {
		t.Errorf("expected authentication message, got: %s", items[0].Text)
	}
	if strings.Contains(items[0].Text, "wrong") {
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
		w.WriteHeader(http.StatusBadGateway)
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
	if !strings.Contains(items[0].Text, "502") {
		t.Errorf("expected HTTP 502 in message, got: %s", items[0].Text)
	}
}

func TestHandleStreamLogs_ConnectionError(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"url":         "http://127.0.0.1:19997",
		"admin_token": "secret-tok",
	})

	items, err := handleStreamLogs(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(items[0].Text, "secret-tok") {
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
		"severity":    "extreme",
	})
	_, err := handleStreamLogs(context.Background(), params)
	if err == nil {
		t.Error("expected an error for invalid severity value")
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

func TestHandleStreamLogs_EmptyEventsNotNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
}

func TestRegisterDefaultTools_IncludesStreamLogs(t *testing.T) {
	srv := newTestServer()
	RegisterDefaultTools(srv)

	if _, ok := srv.handlers["vibewarden_stream_logs"]; !ok {
		t.Error("vibewarden_stream_logs not registered in RegisterDefaultTools")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mustMarshalEvents converts a slice of maps to a slice of json.RawMessage.
func mustMarshalEvents(events []map[string]any) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(events))
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			panic("mustMarshalEvents: " + err.Error())
		}
		result = append(result, b)
	}
	return result
}
