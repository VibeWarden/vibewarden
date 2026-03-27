// Package webhooks implements the VibeWarden webhook delivery plugin.
//
// When enabled, the plugin subscribes to every event emitted by the
// EventLogger pipeline and dispatches matching events to the configured
// HTTP endpoints. Delivery is asynchronous and non-blocking: a failure
// in webhook delivery never stalls request processing.
//
// Three output formats are supported:
//
//   - raw     — the native VibeWarden v1 JSON schema (default)
//   - slack   — Slack Block Kit attachment payload
//   - discord — Discord embed payload
//
// Failed deliveries are retried up to three times with exponential backoff
// (1 s, 5 s, 30 s). After the final failure a dead-letter log entry is emitted.
package webhooks

import (
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/webhook"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Config holds all settings for the webhooks plugin.
// It is derived from config.WebhooksConfig at plugin construction time.
type Config struct {
	// Endpoints is the ordered list of webhook endpoint configurations.
	Endpoints []webhook.DispatcherConfig
}

// FromGlobalConfig converts a config.WebhooksConfig into a plugin Config.
// It sets sensible defaults for omitted fields.
func FromGlobalConfig(c config.WebhooksConfig) Config {
	eps := make([]webhook.DispatcherConfig, 0, len(c.Endpoints))
	for _, ep := range c.Endpoints {
		timeout := time.Duration(ep.TimeoutSeconds) * time.Second
		if ep.TimeoutSeconds <= 0 {
			timeout = 0 // NewDispatcher applies defaultTimeout when <= 0
		}
		eps = append(eps, webhook.DispatcherConfig{
			URL:     ep.URL,
			Events:  ep.Events,
			Format:  ports.WebhookFormat(ep.Format),
			Timeout: timeout,
		})
	}
	return Config{Endpoints: eps}
}
