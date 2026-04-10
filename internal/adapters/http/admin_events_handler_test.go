package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vibehttp "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeRingBuffer implements ports.EventRingBuffer for tests.
type fakeRingBuffer struct {
	stored     []ports.StoredEvent
	querySince uint64
	queryTypes []string
	queryLimit int
}

func (f *fakeRingBuffer) Query(since uint64, types []string, limit int) ([]ports.StoredEvent, uint64) {
	f.querySince = since
	f.queryTypes = types
	f.queryLimit = limit

	var result []ports.StoredEvent
	for _, se := range f.stored {
		if se.Cursor <= since {
			continue
		}
		if len(types) > 0 {
			matched := false
			for _, t := range types {
				if t == se.Event.EventType {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		result = append(result, se)
		if len(result) == limit {
			break
		}
	}

	var cursor uint64
	if len(result) > 0 {
		cursor = result[len(result)-1].Cursor
	} else {
		cursor = since
	}
	return result, cursor
}

func makeStoredEvent(cursor uint64, eventType string) ports.StoredEvent {
	return ports.StoredEvent{
		Cursor: cursor,
		Event: events.Event{
			SchemaVersion: events.SchemaVersion,
			EventType:     eventType,
			Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			AISummary:     "test: " + eventType,
			Payload:       map[string]any{"key": "value"},
		},
	}
}

func newMuxWithRingBuffer(svc ports.AdminService, rb ports.EventRingBuffer) *http.ServeMux {
	mux := http.NewServeMux()
	h := vibehttp.NewAdminHandlers(svc, nil).WithEventRingBuffer(rb)
	h.RegisterRoutes(mux)
	return mux
}

// TestListEvents_Success verifies the happy path: events are returned as JSON
// with the correct cursor.
func TestListEvents_Success(t *testing.T) {
	rb := &fakeRingBuffer{
		stored: []ports.StoredEvent{
			makeStoredEvent(1, events.EventTypeAuthSuccess),
			makeStoredEvent(2, events.EventTypeAuthFailed),
			makeStoredEvent(3, events.EventTypeRateLimitHit),
		},
	}
	mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	evts, ok := resp["events"].([]any)
	if !ok {
		t.Fatal("response missing events array")
	}
	if len(evts) != 3 {
		t.Errorf("events count = %d, want 3", len(evts))
	}

	cursor, ok := resp["cursor"].(float64)
	if !ok {
		t.Fatal("response missing cursor field")
	}
	if uint64(cursor) != 3 {
		t.Errorf("cursor = %v, want 3", cursor)
	}
}

// TestListEvents_NoRingBuffer verifies that a 503 is returned when no ring
// buffer is configured.
func TestListEvents_NoRingBuffer(t *testing.T) {
	mux := newMux(&fakeAdminService{})

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service_unavailable")
}

// TestListEvents_SinceParam verifies that the since query parameter is
// forwarded to the ring buffer.
func TestListEvents_SinceParam(t *testing.T) {
	rb := &fakeRingBuffer{
		stored: []ports.StoredEvent{
			makeStoredEvent(1, events.EventTypeAuthSuccess),
			makeStoredEvent(2, events.EventTypeAuthSuccess),
			makeStoredEvent(3, events.EventTypeAuthSuccess),
		},
	}
	mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events?since=1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rb.querySince != 1 {
		t.Errorf("querySince = %d, want 1", rb.querySince)
	}
}

// TestListEvents_TypeFilter verifies that the type query parameter is parsed
// and forwarded correctly.
func TestListEvents_TypeFilter(t *testing.T) {
	rb := &fakeRingBuffer{
		stored: []ports.StoredEvent{
			makeStoredEvent(1, events.EventTypeAuthFailed),
		},
	}
	mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events?type=auth.failed,rate_limit.hit", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(rb.queryTypes) != 2 {
		t.Fatalf("queryTypes = %v, want 2 elements", rb.queryTypes)
	}
	if rb.queryTypes[0] != "auth.failed" {
		t.Errorf("queryTypes[0] = %q, want auth.failed", rb.queryTypes[0])
	}
	if rb.queryTypes[1] != "rate_limit.hit" {
		t.Errorf("queryTypes[1] = %q, want rate_limit.hit", rb.queryTypes[1])
	}
}

// TestListEvents_LimitParam verifies that the limit query parameter is parsed
// and forwarded correctly, including the cap at maxEventsLimit (500).
func TestListEvents_LimitParam(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantLimit int
	}{
		{"default limit", "", 50},
		{"explicit limit", "limit=10", 10},
		{"limit capped at 500", "limit=9999", 500},
		{"invalid limit defaults", "limit=abc", 50},
		{"zero limit defaults", "limit=0", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := &fakeRingBuffer{}
			mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

			url := "/_vibewarden/admin/events"
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if rb.queryLimit != tt.wantLimit {
				t.Errorf("queryLimit = %d, want %d", rb.queryLimit, tt.wantLimit)
			}
		})
	}
}

// TestListEvents_EmptyBuffer verifies the response shape when no events are
// available.
func TestListEvents_EmptyBuffer(t *testing.T) {
	rb := &fakeRingBuffer{}
	mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	evts, ok := resp["events"].([]any)
	if !ok {
		t.Fatal("response missing events array")
	}
	if len(evts) != 0 {
		t.Errorf("events count = %d, want 0", len(evts))
	}
	if _, hasCursor := resp["cursor"]; !hasCursor {
		t.Error("response missing cursor field")
	}
}

// TestListEvents_EventFieldsInResponse verifies that each event item in the
// response contains the expected fields.
func TestListEvents_EventFieldsInResponse(t *testing.T) {
	rb := &fakeRingBuffer{
		stored: []ports.StoredEvent{
			makeStoredEvent(42, events.EventTypeAuthSuccess),
		},
	}
	mux := newMuxWithRingBuffer(&fakeAdminService{}, rb)

	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	evts := resp["events"].([]any)
	if len(evts) != 1 {
		t.Fatalf("events count = %d, want 1", len(evts))
	}

	item := evts[0].(map[string]any)

	if item["cursor"] != float64(42) {
		t.Errorf("cursor = %v, want 42", item["cursor"])
	}
	if item["event_type"] != events.EventTypeAuthSuccess {
		t.Errorf("event_type = %v, want %q", item["event_type"], events.EventTypeAuthSuccess)
	}
	if item["ai_summary"] == "" {
		t.Error("ai_summary should not be empty")
	}
	if item["timestamp"] == "" {
		t.Error("timestamp should not be empty")
	}
	payload, ok := item["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload should be a map")
	}
	if payload["key"] != "value" {
		t.Errorf("payload.key = %v, want value", payload["key"])
	}
}

// TestListEvents_WithEventRingBufferChaining verifies that WithEventRingBuffer
// preserves existing fields (svc, reloader) on the new AdminHandlers copy.
func TestListEvents_WithEventRingBufferChaining(t *testing.T) {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{Users: nil, Total: 0},
	}
	rb := &fakeRingBuffer{
		stored: []ports.StoredEvent{
			makeStoredEvent(1, events.EventTypeProxyStarted),
		},
	}

	mux := http.NewServeMux()
	h := vibehttp.NewAdminHandlers(svc, nil).WithEventRingBuffer(rb)
	h.RegisterRoutes(mux)

	// Verify events endpoint works.
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify users endpoint still works (svc was preserved).
	req2 := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/users", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("users status = %d, want %d", rec2.Code, http.StatusOK)
	}
}
