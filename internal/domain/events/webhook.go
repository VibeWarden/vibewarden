package events

import (
	"fmt"
	"time"
)

// WebhookSignatureValidParams contains the parameters needed to construct a
// webhook.signature_valid event.
type WebhookSignatureValidParams struct {
	// Path is the URL path of the webhook request.
	Path string

	// Method is the HTTP method of the webhook request.
	Method string

	// Provider identifies the signature format used: "stripe", "github",
	// "slack", "twilio", or "generic".
	Provider string

	// ClientIP is the source IP address of the webhook sender.
	ClientIP string
}

// NewWebhookSignatureValid creates a webhook.signature_valid event indicating
// that an inbound webhook request was authenticated successfully.
func NewWebhookSignatureValid(params WebhookSignatureValidParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeWebhookSignatureValid,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Webhook signature valid for %s %s (provider: %s)",
			params.Method, params.Path, params.Provider,
		),
		Payload: map[string]any{
			"path":      params.Path,
			"method":    params.Method,
			"provider":  params.Provider,
			"client_ip": params.ClientIP,
		},
	}
}

// WebhookSignatureInvalidParams contains the parameters needed to construct a
// webhook.signature_invalid event.
type WebhookSignatureInvalidParams struct {
	// Path is the URL path of the webhook request.
	Path string

	// Method is the HTTP method of the webhook request.
	Method string

	// Provider identifies the signature format used: "stripe", "github",
	// "slack", "twilio", or "generic".
	Provider string

	// Reason is a brief description of why the signature was rejected.
	Reason string

	// ClientIP is the source IP address of the webhook sender.
	ClientIP string
}

// NewWebhookSignatureInvalid creates a webhook.signature_invalid event
// indicating that an inbound webhook request was rejected due to an invalid
// or missing signature.
func NewWebhookSignatureInvalid(params WebhookSignatureInvalidParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeWebhookSignatureInvalid,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Webhook signature invalid for %s %s (provider: %s): %s",
			params.Method, params.Path, params.Provider, params.Reason,
		),
		Payload: map[string]any{
			"path":      params.Path,
			"method":    params.Method,
			"provider":  params.Provider,
			"reason":    params.Reason,
			"client_ip": params.ClientIP,
		},
	}
}
