package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// vibewarden_stream_logs
// ---------------------------------------------------------------------------

// severityOrder maps severity label to a numeric rank for comparison.
// Higher rank means higher severity.
var severityOrder = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// streamLogsToolDef returns the ToolDefinition for vibewarden_stream_logs.
func streamLogsToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_stream_logs",
		Description: "Retrieve filtered structured log events from the VibeWarden sidecar. " +
			"Supports filtering by event type prefix (e.g. \"waf\" matches \"waf.detected\" and \"waf.blocked\"), " +
			"minimum severity level, and a look-back duration. " +
			"The ai_summary field is prominently included in every result event.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"url": {
					Type:        "string",
					Description: "Base URL of the VibeWarden sidecar, e.g. 'https://localhost:8443'.",
				},
				"admin_token": {
					Type:        "string",
					Description: "Bearer token for the admin API (set via admin.token in vibewarden.yaml).",
				},
				"event_type": {
					Type:        "string",
					Description: "Filter by event type prefix. \"waf\" matches \"waf.detected\", \"waf.blocked\", etc. Omit to return all event types.",
				},
				"severity": {
					Type:        "string",
					Description: "Minimum severity to include: \"low\", \"medium\", \"high\", or \"critical\". Omit to return all severities.",
				},
				"since": {
					Type:        "string",
					Description: "Look-back duration, e.g. \"5m\" or \"1h\". Only events newer than now minus this duration are returned. Omit to return the latest events regardless of age.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of events to return after filtering (default 50, max 500).",
				},
			},
			Required: []string{"url", "admin_token"},
		},
	}
}

// streamLogsArgs holds the parsed arguments for vibewarden_stream_logs.
type streamLogsArgs struct {
	URL        string `json:"url"`
	AdminToken string `json:"admin_token"`
	EventType  string `json:"event_type,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Since      string `json:"since,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

// streamLogsResponse is the JSON structure returned by vibewarden_stream_logs.
type streamLogsResponse struct {
	Events  []streamLogEvent `json:"events"`
	Count   int              `json:"count"`
	Filters streamLogFilters `json:"filters"`
}

// streamLogFilters records which filters were applied so the caller can see at a
// glance which criteria were used.
type streamLogFilters struct {
	EventType string `json:"event_type,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Since     string `json:"since,omitempty"`
	Limit     int    `json:"limit"`
}

// streamLogEvent is a single event returned by vibewarden_stream_logs.
// The AISummary field is hoisted to the top level so AI agents see it first.
type streamLogEvent struct {
	AISummary string          `json:"ai_summary"`
	EventType string          `json:"event_type"`
	Timestamp string          `json:"timestamp"`
	Severity  string          `json:"severity,omitempty"`
	Outcome   string          `json:"outcome,omitempty"`
	Raw       json.RawMessage `json:"raw"`
}

// rawEventEnvelope is used to extract well-known top-level fields from a raw
// event JSON object without allocating for the full payload.
type rawEventEnvelope struct {
	AISummary string `json:"ai_summary"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Outcome   string `json:"outcome"`
}

// handleStreamLogs implements the vibewarden_stream_logs MCP tool.
// It fetches events from the sidecar admin API and applies client-side
// filtering by event_type prefix, minimum severity, and since duration.
func handleStreamLogs(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args streamLogsArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	if args.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if args.AdminToken == "" {
		return nil, fmt.Errorf("admin_token is required")
	}

	// Validate severity value if provided.
	if args.Severity != "" {
		if _, ok := severityOrder[args.Severity]; !ok {
			return nil, fmt.Errorf("severity must be one of: low, medium, high, critical; got %q", args.Severity)
		}
	}

	// Parse since duration if provided.
	var sinceTime *time.Time
	if args.Since != "" {
		d, err := time.ParseDuration(args.Since)
		if err != nil {
			return nil, fmt.Errorf("since must be a valid Go duration (e.g. \"5m\", \"1h\"): %w", err)
		}
		t := time.Now().UTC().Add(-d)
		sinceTime = &t
	}

	limit := 50
	if args.Limit != nil {
		limit = *args.Limit
		if limit < 1 {
			limit = 1
		}
		if limit > 500 {
			limit = 500
		}
	}

	// Fetch a larger batch from the endpoint so we have enough raw events to
	// fill the requested limit after client-side filtering.
	fetchLimit := limit * 4
	if fetchLimit < 200 {
		fetchLimit = 200
	}
	if fetchLimit > 500 {
		fetchLimit = 500
	}

	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/events"
	endpoint += "?limit=" + strconv.Itoa(fetchLimit)

	// Use InsecureSkipVerify so the tool works with self-signed TLS certificates,
	// which is the default for local VibeWarden sidecars.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for local sidecar
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return text("Failed to build request to admin events endpoint."), nil
	}

	// Set the bearer token -- it must never appear in any response or error text.
	req.Header.Set("Authorization", "Bearer "+args.AdminToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Do NOT include the raw error as it can contain the URL with token context.
		return text("Cannot reach the sidecar admin API. Ensure the sidecar is running and the url is correct."), nil
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return text("Authentication failed -- check admin token."), nil
	case http.StatusServiceUnavailable:
		return text("Sidecar event ring buffer is not available. The sidecar may still be starting up."), nil
	}

	if resp.StatusCode != http.StatusOK {
		return text(fmt.Sprintf("Admin events endpoint returned HTTP %d. Check that the sidecar is healthy.", resp.StatusCode)), nil
	}

	var payload adminEventsPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return text("Failed to decode events response from sidecar."), nil
	}

	// Apply client-side filters.
	filtered := filterEvents(payload.Events, args.EventType, args.Severity, sinceTime, limit)

	result := streamLogsResponse{
		Events: filtered,
		Count:  len(filtered),
		Filters: streamLogFilters{
			EventType: args.EventType,
			Severity:  args.Severity,
			Since:     args.Since,
			Limit:     limit,
		},
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling stream logs response: %w", err)
	}

	return text(string(out)), nil
}

// filterEvents applies event_type prefix, severity, and since filters to a
// slice of raw events, returning at most limit shaped streamLogEvent values.
func filterEvents(
	raw []json.RawMessage,
	eventTypePrefix string,
	minSeverity string,
	since *time.Time,
	limit int,
) []streamLogEvent {
	minRank := 0
	if minSeverity != "" {
		minRank = severityOrder[minSeverity]
	}

	var result []streamLogEvent

	for _, r := range raw {
		if len(result) >= limit {
			break
		}

		var env rawEventEnvelope
		if err := json.Unmarshal(r, &env); err != nil {
			// Skip malformed events.
			continue
		}

		// Filter by event_type prefix.
		if eventTypePrefix != "" {
			prefix := strings.ToLower(eventTypePrefix)
			et := strings.ToLower(env.EventType)
			if et != prefix && !strings.HasPrefix(et, prefix+".") {
				continue
			}
		}

		// Filter by minimum severity.
		if minRank > 0 {
			eventRank := severityOrder[strings.ToLower(env.Severity)]
			if eventRank < minRank {
				continue
			}
		}

		// Filter by since timestamp.
		if since != nil && env.Timestamp != "" {
			ts, err := time.Parse(time.RFC3339, env.Timestamp)
			if err == nil && ts.Before(*since) {
				continue
			}
		}

		result = append(result, streamLogEvent{
			AISummary: env.AISummary,
			EventType: env.EventType,
			Timestamp: env.Timestamp,
			Severity:  env.Severity,
			Outcome:   env.Outcome,
			Raw:       r,
		})
	}

	if result == nil {
		result = []streamLogEvent{}
	}

	return result
}
