package events_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewTLSACMEChainSkipped(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSACMEChainSkippedParams
	}{
		{
			name: "zerossl skipped because email missing",
			params: events.TLSACMEChainSkippedParams{
				Provider:        "zerossl",
				Reason:          "email_not_configured",
				PrimaryProvider: "letsencrypt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewTLSACMEChainSkipped(tt.params)

			assertEvent(t, e, events.EventTypeTLSACMEChainSkipped)

			if e.Severity != events.SeverityInfo {
				t.Errorf("Severity = %q, want %q", e.Severity, events.SeverityInfo)
			}
			if e.Category != events.CategoryNetwork {
				t.Errorf("Category = %q, want %q", e.Category, events.CategoryNetwork)
			}

			requirePayloadString(t, e.Payload, "provider", tt.params.Provider)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "primary_provider", tt.params.PrimaryProvider)

			if !strings.Contains(e.AISummary, tt.params.Provider) {
				t.Errorf("AISummary %q missing provider %q", e.AISummary, tt.params.Provider)
			}
			if !strings.Contains(e.AISummary, tt.params.Reason) {
				t.Errorf("AISummary %q missing reason %q", e.AISummary, tt.params.Reason)
			}
		})
	}
}

func TestNewTLSACMEChainFallback(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSACMEChainFallbackParams
	}{
		{
			name: "letsencrypt -> zerossl due to upstream_unreachable",
			params: events.TLSACMEChainFallbackParams{
				FromProvider: "letsencrypt",
				ToProvider:   "zerossl",
				Reason:       "upstream_unreachable",
				Domain:       "app.example.com",
			},
		},
		{
			name: "letsencrypt -> zerossl due to rate_limited",
			params: events.TLSACMEChainFallbackParams{
				FromProvider: "letsencrypt",
				ToProvider:   "zerossl",
				Reason:       "rate_limited",
				Domain:       "api.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewTLSACMEChainFallback(tt.params)

			assertEvent(t, e, events.EventTypeTLSACMEChainFallback)

			if e.Severity != events.SeverityMedium {
				t.Errorf("Severity = %q, want %q", e.Severity, events.SeverityMedium)
			}
			if e.Category != events.CategoryNetwork {
				t.Errorf("Category = %q, want %q", e.Category, events.CategoryNetwork)
			}

			requirePayloadString(t, e.Payload, "from_provider", tt.params.FromProvider)
			requirePayloadString(t, e.Payload, "to_provider", tt.params.ToProvider)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "domain", tt.params.Domain)
		})
	}
}

func TestNewTLSACMEChainConfigured(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSACMEChainConfiguredParams
	}{
		{
			name: "letsencrypt with fallback to zerossl",
			params: events.TLSACMEChainConfiguredParams{
				PrimaryProvider: "letsencrypt",
				ResolvedChain:   []string{"letsencrypt", "zerossl"},
				Domain:          "app.example.com",
			},
		},
		{
			name: "letsencrypt alone (no email)",
			params: events.TLSACMEChainConfiguredParams{
				PrimaryProvider: "letsencrypt",
				ResolvedChain:   []string{"letsencrypt"},
				Domain:          "app.example.com",
			},
		},
		{
			name: "explicit buypass",
			params: events.TLSACMEChainConfiguredParams{
				PrimaryProvider: "buypass",
				ResolvedChain:   []string{"buypass"},
				Domain:          "app.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewTLSACMEChainConfigured(tt.params)

			assertEvent(t, e, events.EventTypeTLSACMEChainConfigured)

			if e.Severity != events.SeverityInfo {
				t.Errorf("Severity = %q, want %q", e.Severity, events.SeverityInfo)
			}
			if e.Category != events.CategoryNetwork {
				t.Errorf("Category = %q, want %q", e.Category, events.CategoryNetwork)
			}

			requirePayloadString(t, e.Payload, "primary_provider", tt.params.PrimaryProvider)
			requirePayloadString(t, e.Payload, "domain", tt.params.Domain)

			chain, ok := e.Payload["resolved_chain"].([]string)
			if !ok {
				t.Fatalf("payload.resolved_chain has wrong type %T, want []string", e.Payload["resolved_chain"])
			}
			if len(chain) != len(tt.params.ResolvedChain) {
				t.Fatalf("resolved_chain len = %d, want %d", len(chain), len(tt.params.ResolvedChain))
			}
			for i, got := range chain {
				if got != tt.params.ResolvedChain[i] {
					t.Errorf("resolved_chain[%d] = %q, want %q", i, got, tt.params.ResolvedChain[i])
				}
			}
		})
	}
}

// TestNewTLSACMEChainConfigured_DefensiveCopy asserts that mutating the
// caller's input slice does not mutate the emitted event's payload.
func TestNewTLSACMEChainConfigured_DefensiveCopy(t *testing.T) {
	input := []string{"letsencrypt", "zerossl"}
	e := events.NewTLSACMEChainConfigured(events.TLSACMEChainConfiguredParams{
		PrimaryProvider: "letsencrypt",
		ResolvedChain:   input,
		Domain:          "app.example.com",
	})

	input[0] = "mutated"

	chain, ok := e.Payload["resolved_chain"].([]string)
	if !ok {
		t.Fatalf("resolved_chain wrong type %T", e.Payload["resolved_chain"])
	}
	if chain[0] != "letsencrypt" {
		t.Errorf("resolved_chain[0] = %q, want %q (defensive copy broken)", chain[0], "letsencrypt")
	}
}

func TestNewTLSACMEProviderDeprecated(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSACMEProviderDeprecatedParams
	}{
		{
			name: "buypass deprecated",
			params: events.TLSACMEProviderDeprecatedParams{
				Provider: "buypass",
				Reason:   "directory_returns_403",
				Guidance: "consider provider: letsencrypt with tls.email",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewTLSACMEProviderDeprecated(tt.params)

			assertEvent(t, e, events.EventTypeTLSACMEProviderDeprecated)

			if e.Severity != events.SeverityMedium {
				t.Errorf("Severity = %q, want %q", e.Severity, events.SeverityMedium)
			}
			if e.Category != events.CategoryNetwork {
				t.Errorf("Category = %q, want %q", e.Category, events.CategoryNetwork)
			}

			requirePayloadString(t, e.Payload, "provider", tt.params.Provider)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "guidance", tt.params.Guidance)
		})
	}
}
