// Package schema_test validates that sample events produced by the domain layer
// conform to the published VibeWarden v1 event JSON Schema.
//
// The test round-trips each event constructor through the SlogEventLogger (JSON
// serialisation) and then validates the resulting JSON object against
// schema/v1/event.json using santhosh-tekuri/jsonschema v6 (Apache 2.0).
package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	jsschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// schemaPath returns the absolute path to schema/v1/event.json relative to
// this test file so the test works regardless of the working directory.
func schemaPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "event.json")
}

// compileSchema compiles the event.json schema and returns it.
func compileSchema(t *testing.T) *jsschema.Schema {
	t.Helper()
	c := jsschema.NewCompiler()
	// Enable format assertions (date-time, etc.)
	c.AssertFormat()
	sch, err := c.Compile(schemaPath())
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

// marshalEvent serialises an event to a JSON object via SlogEventLogger so that
// the test exercises the exact same serialisation path as production code.
func marshalEvent(t *testing.T, event events.Event) any {
	t.Helper()
	var buf bytes.Buffer
	logger := log.NewSlogEventLogger(&buf)
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("log event: %v", err)
	}
	inst, err := jsschema.UnmarshalJSON(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("unmarshal logged JSON: %v\nraw: %s", err, buf.String())
	}
	return inst
}

// assertValid validates inst against sch, failing the test on schema violations.
func assertValid(t *testing.T, sch *jsschema.Schema, inst any) {
	t.Helper()
	if err := sch.Validate(inst); err != nil {
		// Pretty-print the raw event for debugging.
		raw, _ := json.MarshalIndent(inst, "", "  ")
		t.Errorf("schema validation failed:\n%v\nevent JSON:\n%s", err, raw)
	}
}

// TestSchemaValidation generates one sample event per event_type and validates
// each against schema/v1/event.json. The test fails when any generated event
// does not conform to the schema.
func TestSchemaValidation(t *testing.T) {
	sch := compileSchema(t)

	// sampleTime is a fixed UTC time used where a time.Time field is needed.
	sampleTime := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event events.Event
	}{
		// --- proxy ---
		{
			name: "proxy.started",
			event: events.NewProxyStarted(events.ProxyStartedParams{
				ListenAddr:             ":443",
				UpstreamAddr:           "localhost:3000",
				TLSEnabled:             true,
				TLSProvider:            "letsencrypt",
				SecurityHeadersEnabled: true,
				Version:                "1.0.0",
			}),
		},
		{
			name: "proxy.kratos_flow",
			event: events.NewProxyKratosFlow(events.ProxyKratosFlowParams{
				Method: "GET",
				Path:   "/self-service/login/browser",
			}),
		},
		// --- auth ---
		{
			name: "auth.success",
			event: events.NewAuthSuccess(events.AuthSuccessParams{
				Method:     "GET",
				Path:       "/api/data",
				SessionID:  "sess-abc123",
				IdentityID: "id-xyz456",
				Email:      "alice@example.com",
			}),
		},
		{
			name: "auth.failed",
			event: events.NewAuthFailed(events.AuthFailedParams{
				Method: "GET",
				Path:   "/dashboard",
				Reason: "missing session cookie",
				Detail: "",
			}),
		},
		{
			name: "auth.provider_unavailable",
			event: events.NewAuthProviderUnavailable(events.AuthProviderUnavailableParams{
				ProviderURL:  "http://kratos:4433",
				Error:        "connection refused",
				AffectedPath: "/api/data",
			}),
		},
		{
			name: "auth.provider_recovered",
			event: events.NewAuthProviderRecovered(events.AuthProviderRecoveredParams{
				ProviderURL: "http://kratos:4433",
			}),
		},
		// --- api key ---
		{
			name: "auth.api_key.success",
			event: events.NewAPIKeySuccess(events.APIKeySuccessParams{
				Method:  "GET",
				Path:    "/api/resource",
				KeyName: "my-key",
				Scopes:  []string{"read", "write"},
			}),
		},
		{
			name: "auth.api_key.failed",
			event: events.NewAPIKeyFailed(events.APIKeyFailedParams{
				Method: "GET",
				Path:   "/api/resource",
				Reason: "missing api key",
			}),
		},
		{
			name: "auth.api_key.forbidden",
			event: events.NewAPIKeyForbidden(events.APIKeyForbiddenParams{
				Method:         "POST",
				Path:           "/admin/users",
				KeyName:        "readonly-key",
				KeyScopes:      []string{"read"},
				RequiredScopes: []string{"admin"},
			}),
		},
		// --- jwt ---
		{
			name: "auth.jwt_valid",
			event: events.NewJWTValid(events.JWTValidParams{
				Method:   "GET",
				Path:     "/api/data",
				Subject:  "user-123",
				Issuer:   "https://idp.example.com",
				Audience: "api.example.com",
			}),
		},
		{
			name: "auth.jwt_invalid",
			event: events.NewJWTInvalid(events.JWTInvalidParams{
				Method: "GET",
				Path:   "/api/data",
				Reason: "invalid_signature",
				Detail: "signature verification failed",
			}),
		},
		{
			name: "auth.jwt_expired",
			event: events.NewJWTExpired(events.JWTExpiredParams{
				Method:    "GET",
				Path:      "/api/data",
				Subject:   "user-123",
				ExpiredAt: sampleTime.Add(-1 * time.Hour),
			}),
		},
		{
			name: "auth.jwks_refresh",
			event: events.NewJWKSRefresh(events.JWKSRefreshParams{
				JWKSURL:  "https://idp.example.com/.well-known/jwks.json",
				KeyCount: 3,
			}),
		},
		{
			name: "auth.jwks_error",
			event: events.NewJWKSError(events.JWKSErrorParams{
				JWKSURL: "https://idp.example.com/.well-known/jwks.json",
				Detail:  "connection timeout",
			}),
		},
		// --- rate limit ---
		{
			name: "rate_limit.hit (ip)",
			event: events.NewRateLimitHit(events.RateLimitHitParams{
				LimitType:         "ip",
				Identifier:        "192.168.1.1",
				RequestsPerSecond: 10,
				Burst:             20,
				RetryAfterSeconds: 3,
				Path:              "/api/data",
				Method:            "GET",
			}),
		},
		{
			name: "rate_limit.hit (user)",
			event: events.NewRateLimitHit(events.RateLimitHitParams{
				LimitType:         "user",
				Identifier:        "user-123",
				RequestsPerSecond: 100,
				Burst:             200,
				RetryAfterSeconds: 1,
				Path:              "/api/data",
				Method:            "GET",
				ClientIP:          "10.0.0.5",
			}),
		},
		{
			name: "rate_limit.unidentified_client",
			event: events.NewRateLimitUnidentified(events.RateLimitUnidentifiedParams{
				Path:   "/api/resource",
				Method: "GET",
			}),
		},
		{
			name: "rate_limit.store_fallback",
			event: events.NewRateLimitStoreFallback(events.RateLimitStoreFallbackParams{
				Reason: "redis: connection refused",
			}),
		},
		{
			name:  "rate_limit.store_recovered",
			event: events.NewRateLimitStoreRecovered(),
		},
		// --- request ---
		{
			name: "request.blocked",
			event: events.NewRequestBlocked(events.RequestBlockedParams{
				Method:    "GET",
				Path:      "/admin",
				Reason:    "IP blocklist match",
				BlockedBy: "ip_blocklist",
				ClientIP:  "1.2.3.4",
			}),
		},
		// --- tls ---
		{
			name: "tls.certificate_issued",
			event: events.NewTLSCertificateIssued(events.TLSCertificateIssuedParams{
				Domain:    "example.com",
				Provider:  "letsencrypt",
				ExpiresAt: "2026-06-26T00:00:00Z",
			}),
		},
		// --- user ---
		{
			name: "user.created",
			event: events.NewUserCreated(events.UserCreatedParams{
				IdentityID: "id-newuser001",
				Email:      "newuser@example.com",
				ActorID:    "admin-001",
			}),
		},
		{
			name: "user.deleted",
			event: events.NewUserDeleted(events.UserDeletedParams{
				IdentityID: "id-olduser007",
				Email:      "leavinguser@example.com",
				ActorID:    "admin-001",
				Reason:     "account requested",
			}),
		},
		{
			name: "user.deactivated",
			event: events.NewUserDeactivated(events.UserDeactivatedParams{
				IdentityID: "id-user001",
				Email:      "user@example.com",
				ActorID:    "admin-001",
				Reason:     "policy violation",
			}),
		},
		// --- audit ---
		{
			name: "audit.log_failure",
			event: events.NewAuditLogFailure(events.AuditLogFailureParams{
				Action: "user.created",
				UserID: "id-xyz",
				Error:  "postgres: connection refused",
			}),
		},
		// --- ip filter ---
		{
			name: "ip_filter.blocked",
			event: events.NewIPFilterBlocked(events.IPFilterBlockedParams{
				ClientIP: "192.168.1.100",
				Mode:     "allowlist",
				Method:   "GET",
				Path:     "/api/data",
			}),
		},
		// --- upstream ---
		{
			name: "upstream.timeout",
			event: events.NewUpstreamTimeout(events.UpstreamTimeoutParams{
				Method:         "GET",
				Path:           "/slow",
				TimeoutSeconds: 30,
				ClientIP:       "10.0.0.1",
			}),
		},
		{
			name: "upstream.retry",
			event: events.NewUpstreamRetry(events.UpstreamRetryParams{
				Method:     "GET",
				Path:       "/api/data",
				Attempt:    1,
				StatusCode: 503,
				ClientIP:   "10.0.0.1",
			}),
		},
		{
			name: "upstream.health_changed (healthy)",
			event: events.NewUpstreamHealthChanged(events.UpstreamHealthChangedParams{
				PreviousStatus:   "unknown",
				NewStatus:        "healthy",
				ConsecutiveCount: 3,
				UpstreamURL:      "http://localhost:3000/health",
			}),
		},
		{
			name: "upstream.health_changed (unhealthy)",
			event: events.NewUpstreamHealthChanged(events.UpstreamHealthChangedParams{
				PreviousStatus:   "healthy",
				NewStatus:        "unhealthy",
				ConsecutiveCount: 5,
				UpstreamURL:      "http://localhost:3000/health",
				LastError:        "connection refused",
			}),
		},
		// --- circuit breaker ---
		{
			name: "circuit_breaker.opened",
			event: events.NewCircuitBreakerOpened(events.CircuitBreakerOpenedParams{
				Threshold:      5,
				TimeoutSeconds: 30,
			}),
		},
		{
			name: "circuit_breaker.half_open",
			event: events.NewCircuitBreakerHalfOpen(events.CircuitBreakerHalfOpenParams{
				TimeoutSeconds: 30,
			}),
		},
		{
			name:  "circuit_breaker.closed",
			event: events.NewCircuitBreakerClosed(),
		},
		// --- egress ---
		{
			name: "egress.request",
			event: events.NewEgressRequest(events.EgressRequestParams{
				Route:   "payments",
				Method:  "POST",
				URL:     "https://api.stripe.com/v1/charges",
				TraceID: "",
			}),
		},
		{
			name: "egress.response",
			event: events.NewEgressResponse(events.EgressResponseParams{
				Route:           "payments",
				Method:          "POST",
				URL:             "https://api.stripe.com/v1/charges",
				StatusCode:      200,
				DurationSeconds: 0.342,
				Attempts:        1,
				TraceID:         "",
			}),
		},
		{
			name: "egress.blocked",
			event: events.NewEgressBlocked(events.EgressBlockedParams{
				Route:   "",
				Method:  "GET",
				URL:     "http://internal.corp/secrets",
				Reason:  "no route matched default deny policy",
				TraceID: "",
			}),
		},
		{
			name: "egress.error",
			event: events.NewEgressError(events.EgressErrorParams{
				Route:    "payments",
				Method:   "POST",
				URL:      "https://api.stripe.com/v1/charges",
				Error:    "connection refused",
				Attempts: 3,
				TraceID:  "",
			}),
		},
		{
			name: "egress.sanitized",
			event: events.NewEgressSanitized(events.EgressSanitizedParams{
				Route:               "analytics",
				Method:              "POST",
				URL:                 "https://analytics.example.com/track",
				RedactedHeaders:     1,
				StrippedQueryParams: 2,
				RedactedBodyFields:  3,
				TraceID:             "",
			}),
		},
		{
			name: "egress.response_invalid",
			event: events.NewEgressResponseInvalid(events.EgressResponseInvalidParams{
				Route:       "api",
				Method:      "GET",
				URL:         "https://api.example.com/data",
				StatusCode:  500,
				ContentType: "text/html",
				Reason:      "status code not allowed",
				TraceID:     "",
			}),
		},
		{
			name: "egress.rate_limit_hit",
			event: events.NewEgressRateLimitHit(events.EgressRateLimitHitParams{
				Route:             "payments",
				Limit:             10.0,
				RetryAfterSeconds: 5.0,
			}),
		},
		{
			name: "egress.circuit_breaker.opened",
			event: events.NewEgressCircuitBreakerOpened(events.EgressCircuitBreakerOpenedParams{
				Route:          "payments",
				Threshold:      3,
				TimeoutSeconds: 60,
			}),
		},
		{
			name: "egress.circuit_breaker.closed",
			event: events.NewEgressCircuitBreakerClosed(events.EgressCircuitBreakerClosedParams{
				Route: "payments",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := marshalEvent(t, tt.event)
			assertValid(t, sch, inst)
		})
	}
}

// TestSchemaRejectsInvalidEvents verifies that the schema correctly rejects
// documents that violate structural constraints.
func TestSchemaRejectsInvalidEvents(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name    string
		jsonStr string
	}{
		{
			name:    "missing schema_version",
			jsonStr: `{"event_type":"auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"ok","payload":{}}`,
		},
		{
			name:    "wrong schema_version",
			jsonStr: `{"schema_version":"v2","event_type":"auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"ok","payload":{}}`,
		},
		{
			name:    "missing required field payload",
			jsonStr: `{"schema_version":"v1","event_type":"auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"ok"}`,
		},
		{
			name:    "ai_summary exceeds 200 chars",
			jsonStr: fmt.Sprintf(`{"schema_version":"v1","event_type":"auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":%q,"payload":{}}`, strings.Repeat("x", 201)),
		},
		{
			name:    "invalid timestamp format",
			jsonStr: `{"schema_version":"v1","event_type":"auth.success","timestamp":"not-a-date","ai_summary":"ok","payload":{}}`,
		},
		{
			name:    "additional top-level property",
			jsonStr: `{"schema_version":"v1","event_type":"auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"ok","payload":{},"extra_field":"bad"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, err := jsschema.UnmarshalJSON(strings.NewReader(tt.jsonStr))
			if err != nil {
				t.Fatalf("unmarshal test JSON: %v", err)
			}
			if err := sch.Validate(inst); err == nil {
				t.Errorf("expected schema validation to fail for %q, but it passed", tt.name)
			}
		})
	}
}

// TestSchemaForwardCompatibility verifies that the schema accepts events with
// unknown event types (issue #508). Consumers built against an older version of
// the schema must not reject events emitted by a newer producer that introduces
// additional event types. Unknown event types must pass validation as long as
// the base structural constraints are satisfied.
func TestSchemaForwardCompatibility(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name    string
		jsonStr string
	}{
		{
			name:    "unknown future event type with empty payload",
			jsonStr: `{"schema_version":"v1","event_type":"newplugin.action","timestamp":"2026-03-28T12:00:00Z","ai_summary":"A future event type","payload":{}}`,
		},
		{
			name:    "unknown future event type with arbitrary payload",
			jsonStr: `{"schema_version":"v1","event_type":"fleet.connected","timestamp":"2026-03-28T12:00:00Z","ai_summary":"Fleet connection established","payload":{"node_id":"n-123","region":"eu-central-1"}}`,
		},
		{
			name:    "unknown three-segment event type",
			jsonStr: `{"schema_version":"v1","event_type":"egress.circuit_breaker.future_state","timestamp":"2026-03-28T12:00:00Z","ai_summary":"Circuit breaker entered a future state","payload":{"route":"payments"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, err := jsschema.UnmarshalJSON(strings.NewReader(tt.jsonStr))
			if err != nil {
				t.Fatalf("unmarshal test JSON: %v", err)
			}
			assertValid(t, sch, inst)
		})
	}
}

// TestSchemaRejectsInvalidEventTypePatterns verifies that event_type values
// that do not match the dot-separated lowercase pattern are still rejected.
func TestSchemaRejectsInvalidEventTypePatterns(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name    string
		jsonStr string
	}{
		{
			name:    "event_type starts with digit",
			jsonStr: `{"schema_version":"v1","event_type":"1plugin.action","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type starts with dot",
			jsonStr: `{"schema_version":"v1","event_type":".auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type ends with dot",
			jsonStr: `{"schema_version":"v1","event_type":"auth.success.","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type contains uppercase",
			jsonStr: `{"schema_version":"v1","event_type":"Auth.success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type is empty string",
			jsonStr: `{"schema_version":"v1","event_type":"","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type contains consecutive dots",
			jsonStr: `{"schema_version":"v1","event_type":"auth..success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
		{
			name:    "event_type contains hyphen",
			jsonStr: `{"schema_version":"v1","event_type":"auth-success","timestamp":"2026-03-28T12:00:00Z","ai_summary":"bad","payload":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, err := jsschema.UnmarshalJSON(strings.NewReader(tt.jsonStr))
			if err != nil {
				t.Fatalf("unmarshal test JSON: %v", err)
			}
			if err := sch.Validate(inst); err == nil {
				t.Errorf("expected schema validation to fail for %q, but it passed", tt.name)
			}
		})
	}
}
