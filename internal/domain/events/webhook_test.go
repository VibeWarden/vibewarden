package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// --- webhook.signature_valid ---

func TestNewWebhookSignatureValid(t *testing.T) {
	tests := []struct {
		name   string
		params events.WebhookSignatureValidParams
	}{
		{
			name: "stripe webhook valid",
			params: events.WebhookSignatureValidParams{
				Path:     "/webhooks/stripe",
				Method:   "POST",
				Provider: "stripe",
				ClientIP: "1.2.3.4",
			},
		},
		{
			name: "github webhook valid",
			params: events.WebhookSignatureValidParams{
				Path:     "/webhooks/github",
				Method:   "POST",
				Provider: "github",
				ClientIP: "140.82.112.1",
			},
		},
		{
			name: "webhook valid no client ip",
			params: events.WebhookSignatureValidParams{
				Path:     "/hooks/slack",
				Method:   "POST",
				Provider: "slack",
				ClientIP: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewWebhookSignatureValid(tt.params)

			assertEvent(t, e, events.EventTypeWebhookSignatureValid)
			requireSummaryContains(t, e.AISummary, tt.params.Provider)
			requireSummaryContains(t, e.AISummary, tt.params.Path)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "provider", tt.params.Provider)
			requirePayloadString(t, e.Payload, "client_ip", tt.params.ClientIP)
		})
	}
}

// --- webhook.signature_invalid ---

func TestNewWebhookSignatureInvalid(t *testing.T) {
	tests := []struct {
		name   string
		params events.WebhookSignatureInvalidParams
	}{
		{
			name: "signature mismatch",
			params: events.WebhookSignatureInvalidParams{
				Path:     "/webhooks/stripe",
				Method:   "POST",
				Provider: "stripe",
				Reason:   "signature mismatch",
				ClientIP: "1.2.3.4",
			},
		},
		{
			name: "missing signature header",
			params: events.WebhookSignatureInvalidParams{
				Path:     "/webhooks/github",
				Method:   "POST",
				Provider: "github",
				Reason:   "signature header missing",
				ClientIP: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewWebhookSignatureInvalid(tt.params)

			assertEvent(t, e, events.EventTypeWebhookSignatureInvalid)
			requireSummaryContains(t, e.AISummary, tt.params.Provider)
			requireSummaryContains(t, e.AISummary, tt.params.Reason)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "provider", tt.params.Provider)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "client_ip", tt.params.ClientIP)
		})
	}
}
