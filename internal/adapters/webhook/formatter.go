// Package webhook provides HTTP webhook delivery adapters for VibeWarden.
// It implements the ports.WebhookDispatcher interface and contains formatters
// for Slack Block Kit, Discord embeds, and raw JSON payloads.
package webhook

import (
	"encoding/json"
	"fmt"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// severityColor maps event types to a display color for rich-format platforms.
// Events with "failed", "blocked", or "unavailable" in their type are red;
// events with "hit" or "unidentified" are yellow; all others are green.
func severityColor(eventType string) string {
	switch {
	case containsAny(eventType, "failed", "blocked", "unavailable"):
		return "danger" // Slack danger (red)
	case containsAny(eventType, "hit", "unidentified"):
		return "warning" // Slack warning (yellow)
	default:
		return "good" // Slack good (green)
	}
}

// severityColorHex returns the hex color code for Discord embeds.
func severityColorHex(eventType string) int {
	switch {
	case containsAny(eventType, "failed", "blocked", "unavailable"):
		return 0xED4245 // Discord red
	case containsAny(eventType, "hit", "unidentified"):
		return 0xFEE75C // Discord yellow
	default:
		return 0x57F287 // Discord green
	}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Raw JSON formatter
// ---------------------------------------------------------------------------

// RawFormatter formats events as the native VibeWarden v1 JSON schema.
// It is the default format when no platform-specific format is configured.
type RawFormatter struct{}

// rawEventJSON is the JSON representation of a VibeWarden event payload.
type rawEventJSON struct {
	SchemaVersion string         `json:"schema_version"`
	EventType     string         `json:"event_type"`
	Timestamp     string         `json:"timestamp"`
	AISummary     string         `json:"ai_summary"`
	Payload       map[string]any `json:"payload"`
}

// Format marshals the event into raw VibeWarden v1 JSON.
func (f *RawFormatter) Format(event events.Event) ([]byte, error) {
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	r := rawEventJSON{
		SchemaVersion: event.SchemaVersion,
		EventType:     event.EventType,
		Timestamp:     event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		AISummary:     event.AISummary,
		Payload:       payload,
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("raw formatter: marshalling event: %w", err)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Slack Block Kit formatter
// ---------------------------------------------------------------------------

// SlackFormatter formats events for Slack's incoming webhook API using Block Kit.
// The attachment color indicates severity: green (info), yellow (warning), red (error).
type SlackFormatter struct{}

// slackPayload is the JSON structure sent to Slack incoming webhooks.
type slackPayload struct {
	Attachments []slackAttachment `json:"attachments"`
}

// slackAttachment is a single Slack message attachment with color coding.
type slackAttachment struct {
	Color  string       `json:"color"`
	Blocks []slackBlock `json:"blocks"`
}

// slackBlock is a Slack Block Kit block element.
type slackBlock struct {
	Type   string      `json:"type"`
	Text   *slackText  `json:"text,omitempty"`
	Fields []slackText `json:"fields,omitempty"`
}

// slackText is a Slack text element within a block.
type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Format converts the event into a Slack Block Kit attachment JSON payload.
func (f *SlackFormatter) Format(event events.Event) ([]byte, error) {
	color := severityColor(event.EventType)

	// Header block with event type as title.
	headerBlock := slackBlock{
		Type: "header",
		Text: &slackText{
			Type: "plain_text",
			Text: fmt.Sprintf("[VibeWarden] %s", event.EventType),
		},
	}

	// Section block with the AI summary.
	sectionBlock := slackBlock{
		Type: "section",
		Text: &slackText{
			Type: "mrkdwn",
			Text: event.AISummary,
		},
	}

	// Fields block with payload key-value pairs (up to 10 fields for readability).
	var fields []slackText
	count := 0
	for k, v := range event.Payload {
		if count >= 10 {
			break
		}
		fields = append(fields, slackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*%s*\n%v", k, v),
		})
		count++
	}

	blocks := []slackBlock{headerBlock, sectionBlock}
	if len(fields) > 0 {
		blocks = append(blocks, slackBlock{
			Type:   "section",
			Fields: fields,
		})
	}

	// Context block with timestamp.
	blocks = append(blocks, slackBlock{
		Type: "context",
		Text: &slackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Timestamp:* %s | *Schema:* %s",
				event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
				event.SchemaVersion,
			),
		},
	})

	payload := slackPayload{
		Attachments: []slackAttachment{
			{
				Color:  color,
				Blocks: blocks,
			},
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("slack formatter: marshalling payload: %w", err)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Discord embed formatter
// ---------------------------------------------------------------------------

// DiscordFormatter formats events for Discord's incoming webhook API using embeds.
// The embed color indicates severity: green (info), yellow (warning), red (error).
type DiscordFormatter struct{}

// discordPayload is the JSON structure sent to Discord incoming webhooks.
type discordPayload struct {
	Username string         `json:"username"`
	Embeds   []discordEmbed `json:"embeds"`
}

// discordEmbed is a single Discord embed object.
type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp"`
}

// discordField is a field within a Discord embed.
type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// discordFooter is the footer of a Discord embed.
type discordFooter struct {
	Text string `json:"text"`
}

// Format converts the event into a Discord incoming webhook JSON payload.
func (f *DiscordFormatter) Format(event events.Event) ([]byte, error) {
	color := severityColorHex(event.EventType)

	// Build fields from the event payload (up to 25 per Discord limits).
	var fields []discordField
	count := 0
	for k, v := range event.Payload {
		if count >= 25 {
			break
		}
		fields = append(fields, discordField{
			Name:   k,
			Value:  fmt.Sprintf("%v", v),
			Inline: true,
		})
		count++
	}

	embed := discordEmbed{
		Title:       fmt.Sprintf("[VibeWarden] %s", event.EventType),
		Description: event.AISummary,
		Color:       color,
		Fields:      fields,
		Footer: &discordFooter{
			Text: fmt.Sprintf("schema: %s", event.SchemaVersion),
		},
		Timestamp: event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
	}

	p := discordPayload{
		Username: "VibeWarden",
		Embeds:   []discordEmbed{embed},
	}

	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("discord formatter: marshalling payload: %w", err)
	}
	return b, nil
}
