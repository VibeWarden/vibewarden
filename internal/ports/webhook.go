// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// WebhookFormat identifies the target platform format for a webhook payload.
type WebhookFormat string

const (
	// WebhookFormatRaw sends the raw VibeWarden event JSON.
	WebhookFormatRaw WebhookFormat = "raw"

	// WebhookFormatSlack formats the event for Slack's Block Kit API.
	WebhookFormatSlack WebhookFormat = "slack"

	// WebhookFormatDiscord formats the event for Discord's embed API.
	WebhookFormatDiscord WebhookFormat = "discord"
)

// WebhookDispatcher is the outbound port for dispatching structured events to
// configured HTTP webhook endpoints. Implementations must be safe for concurrent
// use. Webhook failures must not block the caller — they are best-effort.
type WebhookDispatcher interface {
	// Dispatch sends event to all configured endpoints whose event filter matches.
	// It returns immediately; delivery runs asynchronously in the background.
	// Implementations must honour the context for shutdown signalling.
	Dispatch(ctx context.Context, event events.Event) error
}

// WebhookFormatter is the outbound port for converting a domain event into a
// platform-specific HTTP request body. Each format (Slack, Discord, raw JSON)
// has its own implementation.
type WebhookFormatter interface {
	// Format converts event into the platform-specific JSON payload bytes.
	// Returns an error if the event cannot be marshalled for that platform.
	Format(event events.Event) ([]byte, error)
}
