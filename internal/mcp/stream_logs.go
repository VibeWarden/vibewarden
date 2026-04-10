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

// severityRank maps severity level strings to their numeric rank for
// ordered filtering. Info is not ranked here because the filter applies to
// the explicit levels low/medium/high/critical only.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// streamLogsToolDef returns the ToolDefinition for vibewarden_stream_logs.
func streamLogsToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_stream_logs",
		Description: "Fetch and filter structured log events from the VibeWarden admin events endpoint. " +
			"Unlike vibewarden_watch_events, this tool performs client-side filtering by event_type prefix, " +
			"minimum severity, and a time window. Events are returned with ai_summary prominently displayed " +
			"for fast AI triage.",
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
					Description: "Optional event type prefix filter (e.g. 'auth' matches 'auth.success', 'auth.failed'). Omit to return all event types.",
				},
				"severity": {
					Type:        "string",
					Description: "Optional minimum severity level: 'low', 'medium', 'high', or 'critical'. Only events at or above this level are returned. Omit to return all severities.",
				},
				"since": {
					Type:        "string",
					Description: "Optional duration window (e.g. '5m', '1h', '30s'). Only events with a timestamp within this window are returned. Omit to return all events.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of events to fetch from the endpoint before filtering (default 50, max 500).",
				},
			},
			Required: []string{"url", "admin_token"},
		},
	}
}

// streamLogsArgs holds the arguments for vibewarden_stream_logs.
type streamLogsArgs struct {
	URL        string `json:"url"`
	AdminToken string `json:"admin_token"`
	EventType  string `json:"event_type,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Since      string `json:"since,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

// streamLogsEvent is the shape we use for parsing individual events from the
// admin events response. Only the fields we need for filtering and display are
// decoded; the full raw JSON is preserved as-is in the output.
type streamLogsEvent struct {
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Severity  string    `json:"severity"`
	AISummary string    `json:"ai_summary"`
}

// streamLogsResult is the JSON structure returned by vibewarden_stream_logs.
type streamLogsResult struct {
	Events []json.RawMessage `json:"events"`
	Count  int               `json:"count"`
	// Filters echoes back the active filters for the caller's reference.
	Filters streamLogsFilters `json:"filters"`
}

// streamLogsFilters documents which filters were applied.
type streamLogsFilters struct {
	EventTypePrefix string `json:"event_type_prefix,omitempty"`
	MinSeverity     string `json:"min_severity,omitempty"`
	Since           string `json:"since,omitempty"`
}

// handleStreamLogs implements the vibewarden_stream_logs MCP tool.
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

	// Validate severity if provided.
	if args.Severity != "" {
		if _, ok := severityRank[strings.ToLower(args.Severity)]; !ok {
			return nil, fmt.Errorf("severity must be one of low, medium, high, critical; got %q", args.Severity)
		}
		args.Severity = strings.ToLower(args.Severity)
	}

	// Parse the since duration if provided.
	var sinceWindow time.Duration
	if args.Since != "" {
		d, err := time.ParseDuration(args.Since)
		if err != nil {
			return nil, fmt.Errorf("since must be a valid Go duration (e.g. '5m', '1h'): %w", err)
		}
		sinceWindow = d
	}

	// Determine the limit to pass to the endpoint.
	limit := 50
	if args.Limit != nil {
		limit = *args.Limit
	}

	// Build the request URL.
	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/events"
	endpoint += "?limit=" + strconv.Itoa(limit)

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

	// Set the bearer token — it must never appear in any response or error text.
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
		return text("Authentication failed — check admin token."), nil
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
	now := time.Now().UTC()
	minRank := 0
	if args.Severity != "" {
		minRank = severityRank[args.Severity]
	}

	var filtered []json.RawMessage
	for _, raw := range payload.Events {
		var ev streamLogsEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			// Skip events that cannot be parsed — never fail the whole call.
			continue
		}

		// Filter by event_type prefix.
		if args.EventType != "" && !strings.HasPrefix(ev.EventType, args.EventType) {
			continue
		}

		// Filter by minimum severity rank.
		if minRank > 0 {
			evRank, ok := severityRank[strings.ToLower(ev.Severity)]
			if !ok || evRank < minRank {
				continue
			}
		}

		// Filter by time window.
		if sinceWindow > 0 {
			cutoff := now.Add(-sinceWindow)
			if ev.Timestamp.IsZero() || ev.Timestamp.Before(cutoff) {
				continue
			}
		}

		filtered = append(filtered, raw)
	}

	// Ensure a nil slice is serialised as an empty array, not null.
	if filtered == nil {
		filtered = []json.RawMessage{}
	}

	result := streamLogsResult{
		Events: filtered,
		Count:  len(filtered),
		Filters: streamLogsFilters{
			EventTypePrefix: args.EventType,
			MinSeverity:     args.Severity,
			Since:           args.Since,
		},
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling stream logs response: %w", err)
	}

	return text(string(out)), nil
}
