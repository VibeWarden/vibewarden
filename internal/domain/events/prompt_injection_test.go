package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewLLMPromptInjectionBlocked(t *testing.T) {
	tests := []struct {
		name   string
		params events.LLMPromptInjectionParams
	}{
		{
			name: "block on messages content path",
			params: events.LLMPromptInjectionParams{
				Route:       "openai-chat",
				Method:      "POST",
				URL:         "https://api.openai.com/v1/chat/completions",
				Pattern:     "ignore_previous_instructions",
				ContentPath: ".messages[0].content",
				Action:      "block",
				TraceID:     "trace-abc",
			},
		},
		{
			name: "block on prompt field",
			params: events.LLMPromptInjectionParams{
				Route:       "anthropic-complete",
				Method:      "POST",
				URL:         "https://api.anthropic.com/v1/complete",
				Pattern:     "jailbreak_pattern",
				ContentPath: ".prompt",
				Action:      "block",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewLLMPromptInjectionBlocked(tt.params)

			assertEvent(t, ev, events.EventTypeLLMPromptInjectionBlocked)
			requireSummaryContains(t, ev.AISummary, tt.params.Route)
			requireSummaryContains(t, ev.AISummary, tt.params.Pattern)
			requireSummaryContains(t, ev.AISummary, tt.params.ContentPath)

			// Verify payload fields.
			requirePayloadString(t, ev.Payload, "route", tt.params.Route)
			requirePayloadString(t, ev.Payload, "method", tt.params.Method)
			requirePayloadString(t, ev.Payload, "url", tt.params.URL)
			requirePayloadString(t, ev.Payload, "pattern", tt.params.Pattern)
			requirePayloadString(t, ev.Payload, "content_path", tt.params.ContentPath)
			requirePayloadString(t, ev.Payload, "action", tt.params.Action)

			// trace_id must NOT be in payload (moved to top-level field).
			if _, ok := ev.Payload["trace_id"]; ok {
				t.Error("trace_id should not be in payload — it must be on the top-level Event field")
			}

			// Verify enrichment fields.
			if ev.Actor.Type != events.ActorTypeSystem {
				t.Errorf("Actor.Type = %q, want %q", ev.Actor.Type, events.ActorTypeSystem)
			}
			if ev.Resource.Type != events.ResourceTypeEgressRoute {
				t.Errorf("Resource.Type = %q, want %q", ev.Resource.Type, events.ResourceTypeEgressRoute)
			}
			if ev.Resource.Path != tt.params.Route {
				t.Errorf("Resource.Path = %q, want %q", ev.Resource.Path, tt.params.Route)
			}
			if ev.Resource.Method != tt.params.Method {
				t.Errorf("Resource.Method = %q, want %q", ev.Resource.Method, tt.params.Method)
			}
			if ev.Outcome != events.OutcomeBlocked {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeBlocked)
			}
			if len(ev.RiskSignals) == 0 {
				t.Error("RiskSignals must not be empty for a blocked prompt injection event")
			} else {
				rs := ev.RiskSignals[0]
				if rs.Signal != "prompt_injection" {
					t.Errorf("RiskSignals[0].Signal = %q, want %q", rs.Signal, "prompt_injection")
				}
				if rs.Score != 1.0 {
					t.Errorf("RiskSignals[0].Score = %v, want 1.0", rs.Score)
				}
			}
			if ev.TraceID != tt.params.TraceID {
				t.Errorf("TraceID = %q, want %q", ev.TraceID, tt.params.TraceID)
			}
			if ev.TriggeredBy != "prompt_injection_middleware" {
				t.Errorf("TriggeredBy = %q, want %q", ev.TriggeredBy, "prompt_injection_middleware")
			}
		})
	}
}

func TestNewLLMPromptInjectionDetected(t *testing.T) {
	tests := []struct {
		name   string
		params events.LLMPromptInjectionParams
	}{
		{
			name: "detect on messages content path (log-only)",
			params: events.LLMPromptInjectionParams{
				Route:       "openai-chat",
				Method:      "POST",
				URL:         "https://api.openai.com/v1/chat/completions",
				Pattern:     "ignore_previous_instructions",
				ContentPath: ".messages[0].content",
				Action:      "detect",
				TraceID:     "trace-xyz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewLLMPromptInjectionDetected(tt.params)

			assertEvent(t, ev, events.EventTypeLLMPromptInjectionDetected)
			requireSummaryContains(t, ev.AISummary, tt.params.Route)
			requireSummaryContains(t, ev.AISummary, tt.params.Pattern)

			// Verify payload fields.
			requirePayloadString(t, ev.Payload, "route", tt.params.Route)
			requirePayloadString(t, ev.Payload, "action", tt.params.Action)

			// trace_id must NOT be in payload.
			if _, ok := ev.Payload["trace_id"]; ok {
				t.Error("trace_id should not be in payload — it must be on the top-level Event field")
			}

			// Verify enrichment fields.
			if ev.Actor.Type != events.ActorTypeSystem {
				t.Errorf("Actor.Type = %q, want %q", ev.Actor.Type, events.ActorTypeSystem)
			}
			if ev.Resource.Type != events.ResourceTypeEgressRoute {
				t.Errorf("Resource.Type = %q, want %q", ev.Resource.Type, events.ResourceTypeEgressRoute)
			}
			if ev.Outcome != events.OutcomeAllowed {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
			}
			if len(ev.RiskSignals) == 0 {
				t.Error("RiskSignals must not be empty for a detected prompt injection event")
			} else {
				rs := ev.RiskSignals[0]
				if rs.Signal != "prompt_injection" {
					t.Errorf("RiskSignals[0].Signal = %q, want %q", rs.Signal, "prompt_injection")
				}
				// Detected (log-only) has a lower score than blocked.
				if rs.Score >= 1.0 {
					t.Errorf("RiskSignals[0].Score = %v, want < 1.0 for log-only detection", rs.Score)
				}
			}
			if ev.TraceID != tt.params.TraceID {
				t.Errorf("TraceID = %q, want %q", ev.TraceID, tt.params.TraceID)
			}
			if ev.TriggeredBy != "prompt_injection_middleware" {
				t.Errorf("TriggeredBy = %q, want %q", ev.TriggeredBy, "prompt_injection_middleware")
			}
		})
	}
}
