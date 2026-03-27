package webhooks_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/webhook"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/plugins/webhooks"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// fakeLogger is a fake ports.EventLogger that records calls.
type fakeLogger struct {
	logged []events.Event
	err    error
}

func (f *fakeLogger) Log(_ context.Context, ev events.Event) error {
	if f.err != nil {
		return f.err
	}
	f.logged = append(f.logged, ev)
	return nil
}

func makeEvent(eventType, summary string) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Now().UTC(),
		AISummary:     summary,
		Payload:       map[string]any{},
	}
}

func noEndpointConfig() webhooks.Config {
	return webhooks.Config{Endpoints: nil}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

func TestPlugin_Name(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	if got := p.Name(); got != "webhooks" {
		t.Errorf("Name() = %q, want \"webhooks\"", got)
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestPlugin_Init_NoEndpoints_OK(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false, want true")
	}
}

func TestPlugin_Init_InvalidEndpoint_ReturnsError(t *testing.T) {
	cfg := webhooks.Config{
		Endpoints: []webhook.DispatcherConfig{
			{URL: "", Events: []string{"*"}, Format: ports.WebhookFormatRaw},
		},
	}
	p := webhooks.New(cfg, nil, discardLogger())
	if err := p.Init(context.Background()); err == nil {
		t.Error("Init() expected error for empty URL, got nil")
	}
	h := p.Health()
	if h.Healthy {
		t.Error("Health().Healthy = true, want false after failed init")
	}
}

func TestPlugin_Init_ValidEndpoints_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := webhooks.Config{
		Endpoints: []webhook.DispatcherConfig{
			{
				URL:    srv.URL,
				Events: []string{"*"},
				Format: ports.WebhookFormatRaw,
			},
		},
	}
	p := webhooks.New(cfg, nil, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() unexpected error: %v", err)
	}
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false, want true")
	}
	if !strings.Contains(h.Message, "1 endpoints") {
		t.Errorf("Health().Message = %q, want it to mention \"1 endpoints\"", h.Message)
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func TestPlugin_Start_IsNoop(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	_ = p.Init(context.Background())
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("Start() unexpected error: %v", err)
	}
}

func TestPlugin_Stop_SetsUnhealthy(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	_ = p.Init(context.Background())
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}
	h := p.Health()
	if h.Healthy {
		t.Error("Health().Healthy = true after Stop(), want false")
	}
}

// ---------------------------------------------------------------------------
// Log — forwarding to underlying logger
// ---------------------------------------------------------------------------

func TestPlugin_Log_ForwardsToUnderlying(t *testing.T) {
	fake := &fakeLogger{}
	p := webhooks.New(noEndpointConfig(), fake, discardLogger())
	_ = p.Init(context.Background())

	ev := makeEvent(events.EventTypeProxyStarted, "started")
	if err := p.Log(context.Background(), ev); err != nil {
		t.Fatalf("Log() unexpected error: %v", err)
	}

	if len(fake.logged) != 1 {
		t.Errorf("underlying logger received %d events, want 1", len(fake.logged))
	}
}

func TestPlugin_Log_UnderlyingError_Propagates(t *testing.T) {
	sentinel := errors.New("underlying error")
	fake := &fakeLogger{err: sentinel}
	p := webhooks.New(noEndpointConfig(), fake, discardLogger())
	_ = p.Init(context.Background())

	ev := makeEvent(events.EventTypeProxyStarted, "started")
	err := p.Log(context.Background(), ev)
	if err == nil {
		t.Fatal("Log() expected error from underlying logger, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Log() error = %v, want to wrap %v", err, sentinel)
	}
}

func TestPlugin_Log_NilUnderlying_OK(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	_ = p.Init(context.Background())

	ev := makeEvent(events.EventTypeProxyStarted, "started")
	if err := p.Log(context.Background(), ev); err != nil {
		t.Errorf("Log() unexpected error with nil underlying: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Log — webhook dispatch integration
// ---------------------------------------------------------------------------

func TestPlugin_Log_DispatchesToConfiguredEndpoint(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := webhooks.Config{
		Endpoints: []webhook.DispatcherConfig{
			{
				URL:     srv.URL,
				Events:  []string{"*"},
				Format:  ports.WebhookFormatRaw,
				Timeout: 2 * time.Second,
			},
		},
	}
	p := webhooks.New(cfg, nil, discardLogger())
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	ev := makeEvent(events.EventTypeAuthFailed, "auth failed")
	if err := p.Log(context.Background(), ev); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if received.Load() != 1 {
		t.Errorf("webhook endpoint received %d requests, want 1", received.Load())
	}
}

func TestPlugin_Log_DoesNotDispatchWhenNoEndpoints(t *testing.T) {
	// Verify that no HTTP calls are made when plugin has no endpoints.
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Plugin with no endpoints — should dispatch nothing.
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	_ = p.Init(context.Background())

	ev := makeEvent(events.EventTypeAuthFailed, "auth failed")
	_ = p.Log(context.Background(), ev)

	time.Sleep(100 * time.Millisecond)

	if received.Load() != 0 {
		t.Errorf("received %d unexpected requests to test server", received.Load())
	}
}

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

func TestPlugin_Description_NonEmpty(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	if got := p.Description(); got == "" {
		t.Error("Description() returned empty string")
	}
}

func TestPlugin_ConfigSchema_NonEmpty(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	schema := p.ConfigSchema()
	if len(schema) == 0 {
		t.Error("ConfigSchema() returned empty map")
	}
}

func TestPlugin_Example_ContainsWebhooksKey(t *testing.T) {
	p := webhooks.New(noEndpointConfig(), nil, discardLogger())
	ex := p.Example()
	if !strings.Contains(ex, "webhooks") {
		t.Errorf("Example() = %q, want it to contain \"webhooks\"", ex)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestPlugin_ImplementsPortsPlugin(t *testing.T) {
	var _ ports.Plugin = (*webhooks.Plugin)(nil)
}

func TestPlugin_ImplementsEventLogger(t *testing.T) {
	var _ ports.EventLogger = (*webhooks.Plugin)(nil)
}

func TestPlugin_ImplementsPluginMeta(t *testing.T) {
	var _ ports.PluginMeta = (*webhooks.Plugin)(nil)
}
