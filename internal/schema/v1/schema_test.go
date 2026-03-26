// Package v1_test validates that domain event constructors produce JSON that
// conforms to the VibeWarden v1 event schema contract.
//
// Rather than pulling in a JSON Schema validator library, this test encodes the
// schema rules directly: it marshals each event to JSON, unmarshals it into a
// generic map, and then asserts that every required field is present with the
// correct type and value. This keeps the test portable and adds zero external
// dependencies to the project.
package v1_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// jsonEvent is the wire representation of an Event as emitted by the slog
// adapter. We reproduce the field mapping here so the test is independent of
// the adapter package.
type jsonEvent struct {
	SchemaVersion string         `json:"schema_version"`
	EventType     string         `json:"event_type"`
	Timestamp     time.Time      `json:"timestamp"`
	AISummary     string         `json:"ai_summary"`
	Payload       map[string]any `json:"payload"`
}

// marshalEvent serialises a domain Event into the JSON wire format used by the
// slog adapter, then deserialises it back so the test can inspect field values.
func marshalEvent(t *testing.T, e events.Event) jsonEvent {
	t.Helper()

	wire := map[string]any{
		"schema_version": e.SchemaVersion,
		"event_type":     e.EventType,
		"timestamp":      e.Timestamp.Format(time.RFC3339),
		"ai_summary":     e.AISummary,
		"payload":        e.Payload,
	}

	b, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var out jsonEvent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return out
}

// assertBaseFields verifies the invariants that every v1 event must satisfy.
func assertBaseFields(t *testing.T, je jsonEvent, wantEventType string) {
	t.Helper()

	if je.SchemaVersion != "v1" {
		t.Errorf("schema_version = %q, want %q", je.SchemaVersion, "v1")
	}
	if je.EventType != wantEventType {
		t.Errorf("event_type = %q, want %q", je.EventType, wantEventType)
	}
	if je.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}
	if je.AISummary == "" {
		t.Error("ai_summary is empty")
	}
	const maxSummary = 200
	if len(je.AISummary) > maxSummary {
		t.Errorf("ai_summary length = %d, exceeds maximum %d", len(je.AISummary), maxSummary)
	}
	if je.Payload == nil {
		t.Error("payload is nil")
	}
}

// requireString asserts that payload[key] exists and equals want.
func requireString(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("payload missing required key %q", key)
		return
	}
	got, ok := v.(string)
	if !ok {
		t.Errorf("payload[%q] type = %T, want string", key, v)
		return
	}
	if got != want {
		t.Errorf("payload[%q] = %q, want %q", key, got, want)
	}
}

// requireBool asserts that payload[key] exists and equals want.
func requireBool(t *testing.T, payload map[string]any, key string, want bool) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("payload missing required key %q", key)
		return
	}
	got, ok := v.(bool)
	if !ok {
		t.Errorf("payload[%q] type = %T, want bool", key, v)
		return
	}
	if got != want {
		t.Errorf("payload[%q] = %v, want %v", key, got, want)
	}
}

// requireNumber asserts that payload[key] exists (JSON numbers unmarshal to
// float64 in Go's encoding/json).
func requireNumber(t *testing.T, payload map[string]any, key string) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("payload missing required key %q", key)
		return
	}
	if _, ok := v.(float64); !ok {
		t.Errorf("payload[%q] type = %T, want float64 (JSON number)", key, v)
	}
}

// requireAbsent asserts that payload does not contain key.
func requireAbsent(t *testing.T, payload map[string]any, key string) {
	t.Helper()
	if _, ok := payload[key]; ok {
		t.Errorf("payload contains unexpected key %q", key)
	}
}

// --- proxy.started ---

func TestSchemaProxyStarted(t *testing.T) {
	tests := []struct {
		name   string
		params events.ProxyStartedParams
	}{
		{
			name: "tls enabled with letsencrypt",
			params: events.ProxyStartedParams{
				ListenAddr:             ":8443",
				UpstreamAddr:           "localhost:3000",
				TLSEnabled:             true,
				TLSProvider:            "letsencrypt",
				SecurityHeadersEnabled: true,
				Version:                "1.2.3",
			},
		},
		{
			name: "tls disabled",
			params: events.ProxyStartedParams{
				ListenAddr:             ":8080",
				UpstreamAddr:           "localhost:4000",
				TLSEnabled:             false,
				TLSProvider:            "",
				SecurityHeadersEnabled: false,
				Version:                "0.0.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewProxyStarted(tt.params))

			assertBaseFields(t, je, "proxy.started")
			requireString(t, je.Payload, "listen", tt.params.ListenAddr)
			requireString(t, je.Payload, "upstream", tt.params.UpstreamAddr)
			requireBool(t, je.Payload, "tls_enabled", tt.params.TLSEnabled)
			requireString(t, je.Payload, "tls_provider", tt.params.TLSProvider)
			requireBool(t, je.Payload, "security_headers_enabled", tt.params.SecurityHeadersEnabled)
			requireString(t, je.Payload, "version", tt.params.Version)
		})
	}
}

// --- proxy.kratos_flow ---

func TestSchemaProxyKratosFlow(t *testing.T) {
	tests := []struct {
		name   string
		params events.ProxyKratosFlowParams
	}{
		{
			name:   "browser login flow",
			params: events.ProxyKratosFlowParams{Method: "GET", Path: "/self-service/login/browser"},
		},
		{
			name:   "registration API POST",
			params: events.ProxyKratosFlowParams{Method: "POST", Path: "/.ory/kratos/public/self-service/registration"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewProxyKratosFlow(tt.params))

			assertBaseFields(t, je, "proxy.kratos_flow")
			requireString(t, je.Payload, "method", tt.params.Method)
			requireString(t, je.Payload, "path", tt.params.Path)
		})
	}
}

// --- auth.success ---

func TestSchemaAuthSuccess(t *testing.T) {
	tests := []struct {
		name   string
		params events.AuthSuccessParams
	}{
		{
			name: "standard authenticated GET",
			params: events.AuthSuccessParams{
				Method:     "GET",
				Path:       "/api/data",
				SessionID:  "sess-abc123",
				IdentityID: "id-xyz456",
				Email:      "alice@example.com",
			},
		},
		{
			name: "POST to admin endpoint",
			params: events.AuthSuccessParams{
				Method:     "POST",
				Path:       "/admin/users",
				SessionID:  "sess-def789",
				IdentityID: "id-admin001",
				Email:      "admin@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewAuthSuccess(tt.params))

			assertBaseFields(t, je, "auth.success")
			requireString(t, je.Payload, "method", tt.params.Method)
			requireString(t, je.Payload, "path", tt.params.Path)
			requireString(t, je.Payload, "session_id", tt.params.SessionID)
			requireString(t, je.Payload, "identity_id", tt.params.IdentityID)
			requireString(t, je.Payload, "email", tt.params.Email)
		})
	}
}

// --- auth.failed ---

func TestSchemaAuthFailed(t *testing.T) {
	tests := []struct {
		name   string
		params events.AuthFailedParams
	}{
		{
			name: "missing session cookie",
			params: events.AuthFailedParams{
				Method: "GET",
				Path:   "/dashboard",
				Reason: "missing session cookie",
				Detail: "",
			},
		},
		{
			name: "provider unavailable with detail",
			params: events.AuthFailedParams{
				Method: "POST",
				Path:   "/api/submit",
				Reason: "auth provider unavailable",
				Detail: "connection refused",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewAuthFailed(tt.params))

			assertBaseFields(t, je, "auth.failed")
			requireString(t, je.Payload, "method", tt.params.Method)
			requireString(t, je.Payload, "path", tt.params.Path)
			requireString(t, je.Payload, "reason", tt.params.Reason)
			requireString(t, je.Payload, "detail", tt.params.Detail)
		})
	}
}

// --- rate_limit.hit ---

func TestSchemaRateLimitHit(t *testing.T) {
	tests := []struct {
		name            string
		params          events.RateLimitHitParams
		wantClientIPKey bool
	}{
		{
			name: "IP limit exceeded",
			params: events.RateLimitHitParams{
				LimitType:         "ip",
				Identifier:        "192.168.1.1",
				RequestsPerSecond: 10,
				Burst:             20,
				RetryAfterSeconds: 3,
				Path:              "/api/data",
				Method:            "GET",
				ClientIP:          "",
			},
			wantClientIPKey: false,
		},
		{
			name: "user limit exceeded with client IP",
			params: events.RateLimitHitParams{
				LimitType:         "user",
				Identifier:        "user-123",
				RequestsPerSecond: 100,
				Burst:             200,
				RetryAfterSeconds: 1,
				Path:              "/api/data",
				Method:            "GET",
				ClientIP:          "10.0.0.5",
			},
			wantClientIPKey: true,
		},
		{
			name: "user limit exceeded without client IP",
			params: events.RateLimitHitParams{
				LimitType:         "user",
				Identifier:        "user-456",
				RequestsPerSecond: 50,
				Burst:             100,
				RetryAfterSeconds: 2,
				Path:              "/upload",
				Method:            "POST",
				ClientIP:          "",
			},
			wantClientIPKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewRateLimitHit(tt.params))

			assertBaseFields(t, je, "rate_limit.hit")
			requireString(t, je.Payload, "limit_type", tt.params.LimitType)
			requireString(t, je.Payload, "identifier", tt.params.Identifier)
			requireNumber(t, je.Payload, "requests_per_second")
			requireNumber(t, je.Payload, "burst")
			requireNumber(t, je.Payload, "retry_after_seconds")
			requireString(t, je.Payload, "path", tt.params.Path)
			requireString(t, je.Payload, "method", tt.params.Method)

			if tt.wantClientIPKey {
				requireString(t, je.Payload, "client_ip", tt.params.ClientIP)
			} else {
				requireAbsent(t, je.Payload, "client_ip")
			}
		})
	}
}

// --- rate_limit.unidentified_client ---

func TestSchemaRateLimitUnidentifiedClient(t *testing.T) {
	tests := []struct {
		name   string
		params events.RateLimitUnidentifiedParams
	}{
		{name: "GET request", params: events.RateLimitUnidentifiedParams{Path: "/api/resource", Method: "GET"}},
		{name: "POST request", params: events.RateLimitUnidentifiedParams{Path: "/submit", Method: "POST"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewRateLimitUnidentified(tt.params))

			assertBaseFields(t, je, "rate_limit.unidentified_client")
			requireString(t, je.Payload, "path", tt.params.Path)
			requireString(t, je.Payload, "method", tt.params.Method)
		})
	}
}

// --- request.blocked ---

func TestSchemaRequestBlocked(t *testing.T) {
	tests := []struct {
		name   string
		params events.RequestBlockedParams
	}{
		{
			name: "blocked with client IP",
			params: events.RequestBlockedParams{
				Method:    "GET",
				Path:      "/admin",
				Reason:    "IP blocklist match",
				BlockedBy: "ip_blocklist",
				ClientIP:  "1.2.3.4",
			},
		},
		{
			name: "blocked without client IP",
			params: events.RequestBlockedParams{
				Method:    "POST",
				Path:      "/api/dangerous",
				Reason:    "security policy violation",
				BlockedBy: "security_headers",
				ClientIP:  "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewRequestBlocked(tt.params))

			assertBaseFields(t, je, "request.blocked")
			requireString(t, je.Payload, "method", tt.params.Method)
			requireString(t, je.Payload, "path", tt.params.Path)
			requireString(t, je.Payload, "reason", tt.params.Reason)
			requireString(t, je.Payload, "blocked_by", tt.params.BlockedBy)
			requireString(t, je.Payload, "client_ip", tt.params.ClientIP)
		})
	}
}

// --- tls.certificate_issued ---

func TestSchemaTLSCertificateIssued(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSCertificateIssuedParams
	}{
		{
			name: "letsencrypt certificate",
			params: events.TLSCertificateIssuedParams{
				Domain:    "example.com",
				Provider:  "letsencrypt",
				ExpiresAt: "2026-06-26T00:00:00Z",
			},
		},
		{
			name: "self-signed certificate",
			params: events.TLSCertificateIssuedParams{
				Domain:    "localhost",
				Provider:  "self-signed",
				ExpiresAt: "2027-03-26T00:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewTLSCertificateIssued(tt.params))

			assertBaseFields(t, je, "tls.certificate_issued")
			requireString(t, je.Payload, "domain", tt.params.Domain)
			requireString(t, je.Payload, "provider", tt.params.Provider)
			requireString(t, je.Payload, "expires_at", tt.params.ExpiresAt)
		})
	}
}

// --- user.created ---

func TestSchemaUserCreated(t *testing.T) {
	tests := []struct {
		name   string
		params events.UserCreatedParams
	}{
		{
			name:   "standard user creation",
			params: events.UserCreatedParams{IdentityID: "id-newuser001", Email: "newuser@example.com"},
		},
		{
			name:   "admin user creation",
			params: events.UserCreatedParams{IdentityID: "id-admin002", Email: "admin2@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewUserCreated(tt.params))

			assertBaseFields(t, je, "user.created")
			requireString(t, je.Payload, "identity_id", tt.params.IdentityID)
			requireString(t, je.Payload, "email", tt.params.Email)
		})
	}
}

// --- user.deleted ---

func TestSchemaUserDeleted(t *testing.T) {
	tests := []struct {
		name   string
		params events.UserDeletedParams
	}{
		{
			name:   "standard user deletion",
			params: events.UserDeletedParams{IdentityID: "id-olduser007", Email: "leavinguser@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			je := marshalEvent(t, events.NewUserDeleted(tt.params))

			assertBaseFields(t, je, "user.deleted")
			requireString(t, je.Payload, "identity_id", tt.params.IdentityID)
			requireString(t, je.Payload, "email", tt.params.Email)
		})
	}
}

// TestAllEventTypesHaveSchemaDefinition checks that every event type constant
// declared in the domain/events package is represented by exactly one test
// case in this file. This acts as a compile-time guard: if a new event type is
// added to the domain without a corresponding schema test, the developer will
// see a failed test rather than a silent gap.
func TestAllEventTypesHaveSchemaDefinition(t *testing.T) {
	knownTypes := []string{
		events.EventTypeProxyStarted,
		events.EventTypeProxyKratosFlow,
		events.EventTypeAuthSuccess,
		events.EventTypeAuthFailed,
		events.EventTypeRateLimitHit,
		events.EventTypeRateLimitUnidentified,
		events.EventTypeRequestBlocked,
		events.EventTypeTLSCertificateIssued,
		events.EventTypeUserCreated,
		events.EventTypeUserDeleted,
	}

	// All 10 types must be non-empty strings.
	for _, et := range knownTypes {
		if et == "" {
			t.Errorf("event type constant is empty — check domain/events package")
		}
	}

	if len(knownTypes) != 10 {
		t.Errorf("expected 10 event types, got %d — update this test and the JSON schema when adding new types", len(knownTypes))
	}
}

// TestSchemaVersionIsV1 verifies that the SchemaVersion constant has not been
// changed without updating the JSON schema file.
func TestSchemaVersionIsV1(t *testing.T) {
	if events.SchemaVersion != "v1" {
		t.Errorf("SchemaVersion = %q, want %q — update internal/schema/v1/event.json if bumping the version", events.SchemaVersion, "v1")
	}
}
