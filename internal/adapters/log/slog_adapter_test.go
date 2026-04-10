package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// fixedEvent returns an Event with a fixed timestamp for deterministic tests.
func fixedEvent(eventType, aiSummary string, payload map[string]any) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
		AISummary:     aiSummary,
		Payload:       payload,
	}
}

// logAndDecode writes a single event using a SlogEventLogger and decodes the
// resulting JSON line into a map. It fails the test if the output is not valid
// JSON or if more than one line was written.
func logAndDecode(t *testing.T, event events.Event) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() returned unexpected error: %v", err)
	}

	out := buf.Bytes()
	if len(out) == 0 {
		t.Fatal("Log() produced no output")
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	return result
}

// TestSlogEventLogger_TopLevelFields verifies that all required schema fields
// are present at the top level of the emitted JSON object.
func TestSlogEventLogger_TopLevelFields(t *testing.T) {
	event := fixedEvent(
		events.EventTypeAuthSuccess,
		"Authenticated request allowed: GET /api (identity abc123)",
		map[string]any{
			"method":      "GET",
			"path":        "/api",
			"session_id":  "sess_1",
			"identity_id": "abc123",
			"email":       "user@example.com",
		},
	)

	result := logAndDecode(t, event)

	requiredKeys := []string{"schema_version", "event_type", "timestamp", "ai_summary", "payload"}
	for _, key := range requiredKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("missing required key %q in JSON output", key)
		}
	}

	// Ensure slog's internal keys are not present.
	unwantedKeys := []string{"level", "msg", "time"}
	for _, key := range unwantedKeys {
		if _, ok := result[key]; ok {
			t.Errorf("unexpected key %q found in JSON output", key)
		}
	}
}

// TestSlogEventLogger_FieldValues verifies that schema fields carry the correct
// values from the domain Event.
func TestSlogEventLogger_FieldValues(t *testing.T) {
	ts := time.Date(2026, 3, 26, 15, 30, 0, 0, time.UTC)
	event := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeAuthFailed,
		Timestamp:     ts,
		AISummary:     "Unauthenticated request rejected: missing session cookie",
		Payload: map[string]any{
			"method": "POST",
			"path":   "/login",
			"reason": "missing session cookie",
			"detail": "",
		},
	}

	result := logAndDecode(t, event)

	if got := result["schema_version"]; got != "v1" {
		t.Errorf("schema_version = %v, want %q", got, "v1")
	}
	if got := result["event_type"]; got != events.EventTypeAuthFailed {
		t.Errorf("event_type = %v, want %q", got, events.EventTypeAuthFailed)
	}
	if got := result["ai_summary"]; got != event.AISummary {
		t.Errorf("ai_summary = %v, want %q", got, event.AISummary)
	}
	// timestamp is RFC3339Nano in slog's JSON output.
	if got, ok := result["timestamp"].(string); !ok || got == "" {
		t.Errorf("timestamp is missing or not a string: %v", result["timestamp"])
	}
}

// TestSlogEventLogger_PayloadStructure verifies that the payload is nested as
// a JSON object under the "payload" key.
func TestSlogEventLogger_PayloadStructure(t *testing.T) {
	event := fixedEvent(
		events.EventTypeRateLimitHit,
		"Rate limit exceeded",
		map[string]any{
			"limit_type":          "ip",
			"identifier":          "1.2.3.4",
			"requests_per_second": 10.0,
			"burst":               20,
			"retry_after_seconds": 5,
			"path":                "/api",
			"method":              "GET",
		},
	)

	result := logAndDecode(t, event)

	payload, ok := result["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is not a JSON object, got: %T %v", result["payload"], result["payload"])
	}

	wantKeys := []string{"limit_type", "identifier", "requests_per_second", "burst", "retry_after_seconds", "path", "method"}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing key %q", key)
		}
	}
}

// TestSlogEventLogger_MultipleEventTypes exercises several different event
// types to ensure each produces valid JSON with correct schema_version and
// event_type values.
func TestSlogEventLogger_MultipleEventTypes(t *testing.T) {
	tests := []struct {
		name     string
		event    events.Event
		wantType string
	}{
		{
			name: "proxy started",
			event: fixedEvent(
				events.EventTypeProxyStarted,
				"Reverse proxy listening on :8080, forwarding to localhost:3000",
				map[string]any{
					"listen":                   ":8080",
					"upstream":                 "localhost:3000",
					"tls_enabled":              false,
					"tls_provider":             "",
					"security_headers_enabled": true,
					"version":                  "dev",
				},
			),
			wantType: events.EventTypeProxyStarted,
		},
		{
			name: "auth success",
			event: fixedEvent(
				events.EventTypeAuthSuccess,
				"Authenticated request allowed: GET /dashboard (identity u1)",
				map[string]any{
					"method":      "GET",
					"path":        "/dashboard",
					"session_id":  "sess_abc",
					"identity_id": "u1",
					"email":       "alice@example.com",
				},
			),
			wantType: events.EventTypeAuthSuccess,
		},
		{
			name: "auth failed",
			event: fixedEvent(
				events.EventTypeAuthFailed,
				"Unauthenticated request rejected: missing session cookie",
				map[string]any{
					"method": "GET",
					"path":   "/secret",
					"reason": "missing session cookie",
					"detail": "",
				},
			),
			wantType: events.EventTypeAuthFailed,
		},
		{
			name: "rate limit hit",
			event: fixedEvent(
				events.EventTypeRateLimitHit,
				"Rate limit exceeded for ip 10.0.0.1: 5 requests/second limit reached",
				map[string]any{
					"limit_type":          "ip",
					"identifier":          "10.0.0.1",
					"requests_per_second": 5.0,
					"burst":               10,
					"retry_after_seconds": 2,
					"path":                "/api/data",
					"method":              "POST",
				},
			),
			wantType: events.EventTypeRateLimitHit,
		},
		{
			name: "rate limit unidentified",
			event: fixedEvent(
				events.EventTypeRateLimitUnidentified,
				"Request rejected because the client IP could not be determined",
				map[string]any{
					"path":   "/api",
					"method": "GET",
				},
			),
			wantType: events.EventTypeRateLimitUnidentified,
		},
		{
			name: "request blocked",
			event: fixedEvent(
				events.EventTypeRequestBlocked,
				"Request blocked by ip_blocklist: GET /admin — blocked IP",
				map[string]any{
					"method":     "GET",
					"path":       "/admin",
					"reason":     "blocked IP",
					"blocked_by": "ip_blocklist",
					"client_ip":  "1.2.3.4",
				},
			),
			wantType: events.EventTypeRequestBlocked,
		},
		{
			name: "proxy kratos flow",
			event: fixedEvent(
				events.EventTypeProxyKratosFlow,
				"Request proxied to Kratos self-service API",
				map[string]any{
					"method": "POST",
					"path":   "/self-service/login",
				},
			),
			wantType: events.EventTypeProxyKratosFlow,
		},
		{
			name: "tls certificate issued",
			event: fixedEvent(
				events.EventTypeTLSCertificateIssued,
				"TLS certificate issued for example.com via letsencrypt",
				map[string]any{
					"domain":     "example.com",
					"provider":   "letsencrypt",
					"expires_at": "2026-06-26T00:00:00Z",
				},
			),
			wantType: events.EventTypeTLSCertificateIssued,
		},
		{
			name: "user created",
			event: fixedEvent(
				events.EventTypeUserCreated,
				"New user created: bob@example.com (identity id_bob)",
				map[string]any{
					"identity_id": "id_bob",
					"email":       "bob@example.com",
				},
			),
			wantType: events.EventTypeUserCreated,
		},
		{
			name: "user deleted",
			event: fixedEvent(
				events.EventTypeUserDeleted,
				"User deleted: bob@example.com (identity id_bob)",
				map[string]any{
					"identity_id": "id_bob",
					"email":       "bob@example.com",
				},
			),
			wantType: events.EventTypeUserDeleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logAndDecode(t, tt.event)

			if got := result["schema_version"]; got != "v1" {
				t.Errorf("schema_version = %v, want %q", got, "v1")
			}
			if got := result["event_type"]; got != tt.wantType {
				t.Errorf("event_type = %v, want %q", got, tt.wantType)
			}
			if _, ok := result["payload"].(map[string]any); !ok {
				t.Errorf("payload is not a JSON object: %T", result["payload"])
			}
			if got, ok := result["ai_summary"].(string); !ok || got == "" {
				t.Errorf("ai_summary is missing or empty")
			}
		})
	}
}

// TestSlogEventLogger_NilWriter verifies that passing nil as the writer falls
// back gracefully (uses os.Stdout) and does not panic.
func TestSlogEventLogger_NilWriter(t *testing.T) {
	// We can't easily capture stdout, so we just verify no panic occurs.
	// The real writes go to os.Stdout during this test, which is acceptable.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewSlogEventLogger(nil) panicked: %v", r)
		}
	}()
	logger := log.NewSlogEventLogger(nil)
	if logger == nil {
		t.Error("NewSlogEventLogger(nil) returned nil")
	}
}

// TestSlogEventLogger_TimestampFromEvent verifies that the JSON timestamp
// matches the Timestamp field of the Event, not the current wall-clock time.
func TestSlogEventLogger_TimestampFromEvent(t *testing.T) {
	fixedTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	event := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeProxyStarted,
		Timestamp:     fixedTime,
		AISummary:     "test",
		Payload:       map[string]any{},
	}

	result := logAndDecode(t, event)

	tsStr, ok := result["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp is not a string: %v", result["timestamp"])
	}

	parsed, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", tsStr, err)
	}

	if !parsed.Equal(fixedTime) {
		t.Errorf("timestamp = %v, want %v", parsed, fixedTime)
	}
}

// TestSlogEventLogger_EmptyPayload verifies that an empty payload is emitted
// as an empty JSON object, not null or omitted.
func TestSlogEventLogger_EmptyPayload(t *testing.T) {
	event := fixedEvent(events.EventTypeProxyStarted, "test", map[string]any{})

	result := logAndDecode(t, event)

	payload, ok := result["payload"]
	if !ok {
		t.Fatal("payload key is absent when payload is empty")
	}
	if payload == nil {
		t.Fatal("payload is null, want empty object")
	}
	if _, ok := payload.(map[string]any); !ok {
		t.Errorf("payload is not a JSON object: %T", payload)
	}
}

// captureHandler is an slog.Handler used in tests to record Handle calls.
type captureHandler struct {
	records []slog.Record
}

func (c *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *captureHandler) WithGroup(_ string) slog.Handler      { return c }

// TestSlogEventLogger_AdditionalHandler verifies that when an extra handler is
// provided to NewSlogEventLogger, it receives every log record in addition to
// the JSON writer.
func TestSlogEventLogger_AdditionalHandler(t *testing.T) {
	var buf bytes.Buffer
	extra := &captureHandler{}

	logger := log.NewSlogEventLogger(&buf, extra)

	event := fixedEvent(
		events.EventTypeAuthSuccess,
		"Authenticated request allowed",
		map[string]any{"method": "GET", "path": "/api"},
	)

	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	// JSON writer must have produced output.
	if buf.Len() == 0 {
		t.Error("JSON writer produced no output")
	}

	// Additional handler must have received the record.
	if len(extra.records) != 1 {
		t.Errorf("extra handler received %d records, want 1", len(extra.records))
	}
}

// TestSlogEventLogger_MultipleAdditionalHandlers verifies fan-out to more than
// one extra handler.
func TestSlogEventLogger_MultipleAdditionalHandlers(t *testing.T) {
	var buf bytes.Buffer
	h1 := &captureHandler{}
	h2 := &captureHandler{}

	logger := log.NewSlogEventLogger(&buf, h1, h2)

	event := fixedEvent(events.EventTypeProxyStarted, "Proxy started", map[string]any{})
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	if len(h1.records) != 1 {
		t.Errorf("handler1 received %d records, want 1", len(h1.records))
	}
	if len(h2.records) != 1 {
		t.Errorf("handler2 received %d records, want 1", len(h2.records))
	}
}

// TestSlogEventLogger_NoAdditionalHandlers verifies backward compatibility:
// passing no extra handlers keeps the original JSON-only behaviour.
func TestSlogEventLogger_NoAdditionalHandlers(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)

	event := fixedEvent(events.EventTypeAuthSuccess, "test", map[string]any{})
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("JSON writer produced no output")
	}
}

// traceEvent returns a minimal Event suitable for trace context tests.
func traceEvent() events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     events.EventTypeAuthSuccess,
		Timestamp:     time.Now(),
		AISummary:     "Test event for trace context",
		Payload:       map[string]any{},
	}
}

// logAndDecodeWithCtx writes a single event using the given context and decodes
// the JSON output. It fails the test if the output is not valid JSON.
func logAndDecodeWithCtx(t *testing.T, ctx context.Context, event events.Event) map[string]any { //nolint:revive // t must precede ctx in test helpers by Go testing convention
	t.Helper()
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)
	if err := logger.Log(ctx, event); err != nil {
		t.Fatalf("Log() returned unexpected error: %v", err)
	}
	out := buf.Bytes()
	if len(out) == 0 {
		t.Fatal("Log() produced no output")
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	return result
}

// TestSlogEventLogger_Log_WithTraceContext verifies that when the context carries
// a valid OTel span, the emitted JSON includes trace_id (32 hex chars) and
// span_id (16 hex chars) as top-level fields.
func TestSlogEventLogger_Log_WithTraceContext(t *testing.T) {
	// Use an in-memory exporter so the span is recorded without a live backend.
	exp := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	result := logAndDecodeWithCtx(t, ctx, traceEvent())

	traceID, ok := result["trace_id"].(string)
	if !ok || len(traceID) != 32 {
		t.Errorf("trace_id = %q, want 32-char hex string", traceID)
	}

	spanID, ok := result["span_id"].(string)
	if !ok || len(spanID) != 16 {
		t.Errorf("span_id = %q, want 16-char hex string", spanID)
	}
}

// TestSlogEventLogger_Log_WithoutTraceContext verifies that when the context has
// no span, trace_id and span_id are completely absent from the emitted JSON
// (not present as empty strings).
func TestSlogEventLogger_Log_WithoutTraceContext(t *testing.T) {
	result := logAndDecodeWithCtx(t, context.Background(), traceEvent())

	if _, ok := result["trace_id"]; ok {
		t.Error("trace_id should be absent when no span context, but it was present")
	}
	if _, ok := result["span_id"]; ok {
		t.Error("span_id should be absent when no span context, but it was present")
	}
}

// TestSlogEventLogger_Log_WithInvalidSpanContext verifies that a context carrying
// an invalid span context (zero trace ID / zero span ID) produces no trace fields.
func TestSlogEventLogger_Log_WithInvalidSpanContext(t *testing.T) {
	// Construct a span context where IsValid() returns false.
	// A zero-value SpanContext has an all-zero TraceID and SpanID, which IsValid()
	// treats as invalid.
	invalidSC := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{},
		SpanID:     oteltrace.SpanID{},
		TraceFlags: oteltrace.FlagsSampled,
	})
	ctx := oteltrace.ContextWithSpanContext(context.Background(), invalidSC)

	result := logAndDecodeWithCtx(t, ctx, traceEvent())

	if _, ok := result["trace_id"]; ok {
		t.Error("trace_id should be absent for invalid span context, but it was present")
	}
	if _, ok := result["span_id"]; ok {
		t.Error("span_id should be absent for invalid span context, but it was present")
	}
}

// TestSlogEventLogger_EnrichmentFields verifies that the optional enrichment
// fields (actor, resource, outcome, risk_signals, request_id, trace_id,
// triggered_by) are serialized to the top-level JSON object when present, and
// are absent when not set.
func TestSlogEventLogger_EnrichmentFields(t *testing.T) {
	tests := []struct {
		name          string
		event         events.Event
		wantActor     bool
		wantResource  bool
		wantOutcome   string
		wantRiskLen   int
		wantRequestID string
		wantTraceID   string
		wantTriggered string
	}{
		{
			name: "auth success — all enrichment fields present",
			event: events.NewAuthSuccess(events.AuthSuccessParams{
				Method:     "GET",
				Path:       "/api/data",
				SessionID:  "sess-1",
				IdentityID: "user-1",
				Email:      "a@example.com",
				ClientIP:   "10.0.0.1",
				RequestID:  "req-abc",
				TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
			}),
			wantActor:     true,
			wantResource:  true,
			wantOutcome:   "allowed",
			wantRiskLen:   0,
			wantRequestID: "req-abc",
			wantTraceID:   "4bf92f3577b34da6a3ce929d0e0e4736",
			wantTriggered: "auth_middleware",
		},
		{
			name: "rate limit hit — risk signal present",
			event: events.NewRateLimitHit(events.RateLimitHitParams{
				LimitType:         "ip",
				Identifier:        "192.168.1.1",
				RequestsPerSecond: 5,
				Burst:             10,
				RetryAfterSeconds: 1,
				Path:              "/api",
				Method:            "POST",
			}),
			wantActor:     true,
			wantResource:  true,
			wantOutcome:   "rate_limited",
			wantRiskLen:   1,
			wantTriggered: "rate_limit_middleware",
		},
		{
			name: "base event with no enrichment — fields absent",
			event: fixedEvent(
				events.EventTypeProxyStarted,
				"Reverse proxy started",
				map[string]any{"listen": ":8080", "upstream": "localhost:3000"},
			),
			wantActor:    false,
			wantResource: false,
		},
		{
			name: "config reloaded — system actor",
			event: events.NewConfigReloaded(events.ConfigReloadedParams{
				ConfigPath:    "vibewarden.yaml",
				TriggerSource: "file_watcher",
				DurationMS:    20,
			}),
			wantActor:     true,
			wantResource:  true,
			wantOutcome:   "allowed",
			wantTriggered: "file_watcher",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logAndDecode(t, tt.event)

			// actor
			_, hasActor := result["actor"]
			if hasActor != tt.wantActor {
				t.Errorf("actor present = %v, want %v", hasActor, tt.wantActor)
			}

			// resource
			_, hasResource := result["resource"]
			if hasResource != tt.wantResource {
				t.Errorf("resource present = %v, want %v", hasResource, tt.wantResource)
			}

			// outcome
			if tt.wantOutcome != "" {
				got, ok := result["outcome"].(string)
				if !ok {
					t.Errorf("outcome is not a string: %T", result["outcome"])
				} else if got != tt.wantOutcome {
					t.Errorf("outcome = %q, want %q", got, tt.wantOutcome)
				}
			} else {
				if _, ok := result["outcome"]; ok {
					t.Error("outcome should be absent but was present")
				}
			}

			// risk_signals
			if tt.wantRiskLen > 0 {
				rs, ok := result["risk_signals"].([]any)
				if !ok {
					t.Errorf("risk_signals is not an array: %T", result["risk_signals"])
				} else if len(rs) != tt.wantRiskLen {
					t.Errorf("risk_signals length = %d, want %d", len(rs), tt.wantRiskLen)
				}
			}

			// request_id
			if tt.wantRequestID != "" {
				if got, ok := result["request_id"].(string); !ok || got != tt.wantRequestID {
					t.Errorf("request_id = %v, want %q", result["request_id"], tt.wantRequestID)
				}
			}

			// trace_id
			if tt.wantTraceID != "" {
				if got, ok := result["trace_id"].(string); !ok || got != tt.wantTraceID {
					t.Errorf("trace_id = %v, want %q", result["trace_id"], tt.wantTraceID)
				}
			}

			// triggered_by
			if tt.wantTriggered != "" {
				if got, ok := result["triggered_by"].(string); !ok || got != tt.wantTriggered {
					t.Errorf("triggered_by = %v, want %q", result["triggered_by"], tt.wantTriggered)
				}
			}
		})
	}
}

// TestSlogEventLogger_ActorFields verifies that the actor object is correctly
// serialized with its type, id, and optional ip fields.
func TestSlogEventLogger_ActorFields(t *testing.T) {
	event := events.NewAuthSuccess(events.AuthSuccessParams{
		Method:     "GET",
		Path:       "/",
		IdentityID: "user-42",
		ClientIP:   "10.1.2.3",
	})

	result := logAndDecode(t, event)

	actor, ok := result["actor"].(map[string]any)
	if !ok {
		t.Fatalf("actor is not a JSON object: %T", result["actor"])
	}

	if got := actor["type"]; got != "user" {
		t.Errorf("actor.type = %v, want %q", got, "user")
	}
	if got := actor["id"]; got != "user-42" {
		t.Errorf("actor.id = %v, want %q", got, "user-42")
	}
	if got := actor["ip"]; got != "10.1.2.3" {
		t.Errorf("actor.ip = %v, want %q", got, "10.1.2.3")
	}
}

// TestSlogEventLogger_ResourceFields verifies that the resource object is
// correctly serialized with its type, path, and optional method fields.
func TestSlogEventLogger_ResourceFields(t *testing.T) {
	event := events.NewAuthFailed(events.AuthFailedParams{
		Method:   "POST",
		Path:     "/api/submit",
		Reason:   "missing session",
		ClientIP: "1.2.3.4",
	})

	result := logAndDecode(t, event)

	resource, ok := result["resource"].(map[string]any)
	if !ok {
		t.Fatalf("resource is not a JSON object: %T", result["resource"])
	}

	if got := resource["type"]; got != "http_endpoint" {
		t.Errorf("resource.type = %v, want %q", got, "http_endpoint")
	}
	if got := resource["path"]; got != "/api/submit" {
		t.Errorf("resource.path = %v, want %q", got, "/api/submit")
	}
	if got := resource["method"]; got != "POST" {
		t.Errorf("resource.method = %v, want %q", got, "POST")
	}
}

// TestSlogEventLogger_DomainTraceIDPreferredOverOTel verifies that when the
// domain Event carries a TraceID set by the constructor, it is emitted as the
// trace_id field even if no OTel span is active in the context.
func TestSlogEventLogger_DomainTraceIDPreferredOverOTel(t *testing.T) {
	const domainTraceID = "aabbccddeeff00112233445566778899"
	event := events.NewAuthSuccess(events.AuthSuccessParams{
		Method:     "GET",
		Path:       "/",
		IdentityID: "u1",
		TraceID:    domainTraceID,
	})

	result := logAndDecodeWithCtx(t, context.Background(), event)

	got, ok := result["trace_id"].(string)
	if !ok || got != domainTraceID {
		t.Errorf("trace_id = %v, want %q", result["trace_id"], domainTraceID)
	}
}
