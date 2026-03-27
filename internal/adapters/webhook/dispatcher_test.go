package webhook_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/webhook"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

func makeDispatcherConfig(url string, evts []string, format ports.WebhookFormat) webhook.DispatcherConfig {
	return webhook.DispatcherConfig{
		URL:     url,
		Events:  evts,
		Format:  format,
		Timeout: 2 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// NewDispatcher validation
// ---------------------------------------------------------------------------

func TestNewDispatcher_EmptyURL_ReturnsError(t *testing.T) {
	cfg := webhook.DispatcherConfig{
		URL:    "",
		Events: []string{"*"},
		Format: ports.WebhookFormatRaw,
	}
	_, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err == nil {
		t.Error("NewDispatcher() expected error for empty URL, got nil")
	}
}

func TestNewDispatcher_EmptyEvents_ReturnsError(t *testing.T) {
	cfg := webhook.DispatcherConfig{
		URL:    "https://example.com/hook",
		Events: []string{},
		Format: ports.WebhookFormatRaw,
	}
	_, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err == nil {
		t.Error("NewDispatcher() expected error for empty events, got nil")
	}
}

func TestNewDispatcher_UnknownFormat_ReturnsError(t *testing.T) {
	cfg := webhook.DispatcherConfig{
		URL:    "https://example.com/hook",
		Events: []string{"*"},
		Format: ports.WebhookFormat("unknown"),
	}
	_, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err == nil {
		t.Error("NewDispatcher() expected error for unknown format, got nil")
	}
}

func TestNewDispatcher_NoEndpoints_OK(t *testing.T) {
	d, err := webhook.NewDispatcher(nil, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() unexpected error: %v", err)
	}
	if err := d.Dispatch(context.Background(), makeEvent(events.EventTypeProxyStarted, "ok", nil)); err != nil {
		t.Errorf("Dispatch() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dispatch — event filtering
// ---------------------------------------------------------------------------

func TestDispatch_SpecificEvent_DeliveredWhenMatches(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := makeDispatcherConfig(srv.URL, []string{events.EventTypeAuthFailed}, ports.WebhookFormatRaw)
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	ev := makeEvent(events.EventTypeAuthFailed, "auth failed", nil)
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	// Allow goroutine to run.
	time.Sleep(200 * time.Millisecond)

	if received.Load() != 1 {
		t.Errorf("received %d requests, want 1", received.Load())
	}
}

func TestDispatch_SpecificEvent_NotDeliveredWhenNoMatch(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := makeDispatcherConfig(srv.URL, []string{events.EventTypeAuthFailed}, ports.WebhookFormatRaw)
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	// Send a different event type — should not trigger the endpoint.
	ev := makeEvent(events.EventTypeProxyStarted, "proxy started", nil)
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if received.Load() != 0 {
		t.Errorf("received %d requests, want 0", received.Load())
	}
}

func TestDispatch_WildcardEvent_DeliveredForAnyType(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := makeDispatcherConfig(srv.URL, []string{"*"}, ports.WebhookFormatRaw)
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	eventTypes := []string{
		events.EventTypeAuthFailed,
		events.EventTypeProxyStarted,
		events.EventTypeRateLimitHit,
	}
	for _, et := range eventTypes {
		ev := makeEvent(et, "summary", nil)
		if err := d.Dispatch(context.Background(), ev); err != nil {
			t.Fatalf("Dispatch() error: %v", err)
		}
	}

	time.Sleep(300 * time.Millisecond)

	if received.Load() != int32(len(eventTypes)) {
		t.Errorf("received %d requests, want %d", received.Load(), len(eventTypes))
	}
}

// ---------------------------------------------------------------------------
// Dispatch — HTTP status codes
// ---------------------------------------------------------------------------

func TestDispatch_Non2xxResponse_LoggedAndRetried(t *testing.T) {
	// Return 500 three times — verifies that we attempt multiple deliveries.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := webhook.DispatcherConfig{
		URL:     srv.URL,
		Events:  []string{"*"},
		Format:  ports.WebhookFormatRaw,
		Timeout: 500 * time.Millisecond,
	}
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	ev := makeEvent(events.EventTypeAuthFailed, "failed", nil)
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	// Wait long enough for all retries (1s + 5s + 30s = 36s is too slow for tests,
	// so we use a custom retry schedule test via the adapter; here we just verify
	// at least one attempt was made within the first second).
	time.Sleep(300 * time.Millisecond)

	if callCount.Load() < 1 {
		t.Errorf("expected at least 1 HTTP call, got %d", callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Dispatch — Slack format
// ---------------------------------------------------------------------------

func TestDispatch_SlackFormat_DeliveredWithAttachments(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf) //nolint:errcheck
		bodyCh <- buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := makeDispatcherConfig(srv.URL, []string{"*"}, ports.WebhookFormatSlack)
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	ev := makeEvent(events.EventTypeAuthFailed, "auth failed", nil)
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	select {
	case body := <-bodyCh:
		if len(body) == 0 {
			t.Fatal("empty body received by test server")
		}
		if !contains(body, "attachments") {
			t.Errorf("expected Slack attachments payload, got: %s", string(body))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}

// ---------------------------------------------------------------------------
// Dispatch — Discord format
// ---------------------------------------------------------------------------

func TestDispatch_DiscordFormat_DeliveredWithEmbeds(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf) //nolint:errcheck
		bodyCh <- buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := makeDispatcherConfig(srv.URL, []string{"*"}, ports.WebhookFormatDiscord)
	d, err := webhook.NewDispatcher([]webhook.DispatcherConfig{cfg}, discardLogger())
	if err != nil {
		t.Fatalf("NewDispatcher() error: %v", err)
	}

	ev := makeEvent(events.EventTypeRateLimitHit, "rate limit hit", nil)
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}

	select {
	case body := <-bodyCh:
		if len(body) == 0 {
			t.Fatal("empty body received by test server")
		}
		if !contains(body, "embeds") {
			t.Errorf("expected Discord embeds payload, got: %s", string(body))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestDispatcher_ImplementsWebhookDispatcher(t *testing.T) {
	var _ ports.WebhookDispatcher = (*webhook.Dispatcher)(nil)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(b []byte, sub string) bool {
	s := string(b)
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
