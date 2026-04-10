package webhooks

import "github.com/vibewarden/vibewarden/internal/ports"

// Description returns a short description of the webhooks plugin.
func (p *Plugin) Description() string {
	return "Webhook delivery: dispatch security events to Slack, Discord, or any HTTP endpoint"
}

// ConfigSchema returns the configuration field descriptions for the webhooks plugin.
func (p *Plugin) ConfigSchema() map[string]string {
	return map[string]string{
		"endpoints[].url":             "HTTP(S) URL to POST events to (required)",
		"endpoints[].events":          "List of event types to send, or [\"*\"] for all events",
		"endpoints[].format":          "Payload format: \"raw\" (default), \"slack\", or \"discord\"",
		"endpoints[].timeout_seconds": "Per-request HTTP timeout in seconds (default: 10)",
	}
}

// Example returns an example YAML configuration for the webhooks plugin.
func (p *Plugin) Example() string {
	return `  webhooks:
    endpoints:
      - url: https://hooks.slack.com/services/xxx/yyy/zzz
        events: ["auth.failed", "rate_limit.hit"]
        format: slack
      - url: https://discord.com/api/webhooks/xxx/yyy
        events: ["*"]
        format: discord`
}

// Interface guard — webhooks Plugin implements PluginMeta.
var _ ports.PluginMeta = (*Plugin)(nil)
