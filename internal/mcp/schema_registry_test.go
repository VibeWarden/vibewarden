package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestEventTypeRegistryCompleteness verifies that every event type constant
// defined in internal/domain/events is present in the registry.
// The values below must stay in sync with events.go and the per-subsystem files.
func TestEventTypeRegistryCompleteness(t *testing.T) {
	// All event type string values sourced from internal/domain/events/*.go.
	knownEventTypes := []string{
		// events.go
		"proxy.started",
		"proxy.kratos_flow",
		"auth.success",
		"auth.failed",
		"rate_limit.hit",
		"rate_limit.unidentified_client",
		"request.blocked",
		"tls.certificate_issued",
		"user.created",
		"user.deleted",
		"user.deactivated",
		"auth.provider_unavailable",
		"auth.provider_recovered",
		"audit.log_failure",
		"ip_filter.blocked",
		"secret.rotated",
		"secret.rotation_failed",
		"secret.health_check",
		"rate_limit.store_fallback",
		"rate_limit.store_recovered",
		"upstream.timeout",
		"upstream.retry",
		"auth.api_key.success",
		"auth.api_key.failed",
		"auth.api_key.forbidden",
		"maintenance.request_blocked",
		"tls.cert_expiry_warning",
		"tls.cert_expiry_critical",
		"config.reloaded",
		"config.reload_failed",
		"webhook.signature_valid",
		"webhook.signature_invalid",
		// egress.go
		"egress.request",
		"egress.response",
		"egress.blocked",
		"egress.error",
		"egress.circuit_breaker.opened",
		"egress.circuit_breaker.closed",
		"egress.response_invalid",
		"egress.rate_limit_hit",
		"egress.sanitized",
		// circuit_breaker.go
		"circuit_breaker.opened",
		"circuit_breaker.half_open",
		"circuit_breaker.closed",
		// upstream_health.go
		"upstream.health_changed",
		// jwt.go
		"auth.jwt_valid",
		"auth.jwt_invalid",
		"auth.jwt_expired",
		"auth.jwks_refresh",
		"auth.jwks_error",
		// prompt_injection.go
		"llm.prompt_injection_blocked",
		"llm.prompt_injection_detected",
		// llm_response_invalid.go
		"llm.response_invalid",
		// proposal.go
		"agent.proposal_created",
		"agent.proposal_approved",
		"agent.proposal_dismissed",
	}

	for _, et := range knownEventTypes {
		t.Run(et, func(t *testing.T) {
			_, ok := LookupEventType(et)
			if !ok {
				t.Errorf("event type %q is defined in domain/events but missing from the registry", et)
			}
		})
	}
}

func TestAllEventTypesReturnsSortedSlice(t *testing.T) {
	types := AllEventTypes()
	if len(types) == 0 {
		t.Fatal("AllEventTypes returned empty slice")
	}

	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("AllEventTypes is not sorted: %q at index %d is before %q at index %d",
				types[i-1], i-1, types[i], i)
		}
	}
}

func TestLookupEventTypeFound(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		wantDesc  string
	}{
		{
			name:      "auth success",
			eventType: "auth.success",
			wantDesc:  "Kratos session",
		},
		{
			name:      "rate limit hit",
			eventType: "rate_limit.hit",
			wantDesc:  "rate limit",
		},
		{
			name:      "egress request",
			eventType: "egress.request",
			wantDesc:  "egress proxy",
		},
		{
			name:      "prompt injection blocked",
			eventType: "llm.prompt_injection_blocked",
			wantDesc:  "prompt injection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := LookupEventType(tt.eventType)
			if !ok {
				t.Fatalf("LookupEventType(%q) not found", tt.eventType)
			}
			if !strings.Contains(strings.ToLower(info.Description), strings.ToLower(tt.wantDesc)) {
				t.Errorf("LookupEventType(%q).Description = %q, want it to contain %q",
					tt.eventType, info.Description, tt.wantDesc)
			}
		})
	}
}

func TestLookupEventTypeNotFound(t *testing.T) {
	_, ok := LookupEventType("nonexistent.event_type")
	if ok {
		t.Error("LookupEventType returned ok=true for unknown event type")
	}
}

func TestAllRegistryEntriesHaveNonEmptyDescriptions(t *testing.T) {
	for _, et := range AllEventTypes() {
		info := eventTypeRegistry[et]
		if info.Description == "" {
			t.Errorf("event type %q has empty Description", et)
		}
	}
}

func TestAllFieldInfoHaveNonEmptyNames(t *testing.T) {
	for _, et := range AllEventTypes() {
		info := eventTypeRegistry[et]
		for i, f := range info.Fields {
			if f.Name == "" {
				t.Errorf("event type %q field[%d] has empty Name", et, i)
			}
			if f.Type == "" {
				t.Errorf("event type %q field %q has empty Type", et, f.Name)
			}
			if f.Description == "" {
				t.Errorf("event type %q field %q has empty Description", et, f.Name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MCP tool handler tests
// ---------------------------------------------------------------------------

func TestHandleSchemaDescribeNoArgs(t *testing.T) {
	items, err := handleSchemaDescribe(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleSchemaDescribe(nil) unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(items))
	}

	output := items[0].Text
	mustContain := []string{
		"VibeWarden",
		"schema_version",
		"ai_summary",
		"auth.success",
		"rate_limit.hit",
		"egress.request",
		"llm.prompt_injection_blocked",
		"agent.proposal_created",
	}
	for _, want := range mustContain {
		if !strings.Contains(output, want) {
			t.Errorf("handleSchemaDescribe output missing %q", want)
		}
	}
}

func TestHandleSchemaDescribeWithKnownEventType(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		mustHave  []string
	}{
		{
			name:      "auth.success returns fields",
			eventType: "auth.success",
			mustHave:  []string{"auth.success", "session_id", "identity_id", "outcome"},
		},
		{
			name:      "rate_limit.hit returns fields",
			eventType: "rate_limit.hit",
			mustHave:  []string{"rate_limit.hit", "limit_type", "identifier", "retry_after_seconds"},
		},
		{
			name:      "circuit_breaker.closed has no payload fields note",
			eventType: "circuit_breaker.closed",
			mustHave:  []string{"circuit_breaker.closed", "no additional fields"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]string{"event_type": tt.eventType})
			items, err := handleSchemaDescribe(context.Background(), params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("expected 1 content item, got %d", len(items))
			}
			output := items[0].Text
			for _, want := range tt.mustHave {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, output)
				}
			}
		})
	}
}

func TestHandleSchemaDescribeUnknownEventType(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"event_type": "unknown.event"})
	items, err := handleSchemaDescribe(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(items))
	}
	if !strings.Contains(items[0].Text, "Unknown event type") {
		t.Errorf("expected 'Unknown event type' in response, got: %s", items[0].Text)
	}
}

func TestHandleSchemaDescribeInvalidJSON(t *testing.T) {
	_, err := handleSchemaDescribe(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON params, got nil")
	}
}
