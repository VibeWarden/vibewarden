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
// vibewarden_watch_events
// ---------------------------------------------------------------------------

func watchEventsToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_watch_events",
		Description: "Poll the VibeWarden admin events endpoint and return structured log events. " +
			"Call in a loop, passing the returned cursor as since_cursor to receive only new events. " +
			"Events include the event type, AI summary, timestamp, and payload.",
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
				"since_cursor": {
					Type:        "number",
					Description: "Cursor value from a previous call; only events newer than this cursor are returned. Omit to retrieve the latest events.",
				},
				"types": {
					Type:        "string",
					Description: "Comma-separated list of event types to filter by, e.g. 'request,auth_failure'. Omit to return all types.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of events to return (default 50, max 500).",
				},
			},
			Required: []string{"url", "admin_token"},
		},
	}
}

// watchEventsArgs holds the arguments for vibewarden_watch_events.
type watchEventsArgs struct {
	URL         string  `json:"url"`
	AdminToken  string  `json:"admin_token"`
	SinceCursor *uint64 `json:"since_cursor,omitempty"`
	Types       string  `json:"types"`
	Limit       *int    `json:"limit,omitempty"`
}

// watchEventsResponse is the JSON structure returned by vibewarden_watch_events.
type watchEventsResponse struct {
	Events []json.RawMessage `json:"events"`
	Cursor uint64            `json:"cursor"`
	Count  int               `json:"count"`
}

// adminEventsPayload mirrors the JSON shape returned by the sidecar's
// GET /_vibewarden/admin/events endpoint.
type adminEventsPayload struct {
	Events []json.RawMessage `json:"events"`
	Cursor uint64            `json:"cursor"`
}

func handleWatchEvents(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args watchEventsArgs
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

	// Build the request URL with query parameters.
	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/events"

	query := make([]string, 0, 3)
	if args.SinceCursor != nil {
		query = append(query, "since="+strconv.FormatUint(*args.SinceCursor, 10))
	}
	if args.Types != "" {
		query = append(query, "type="+args.Types)
	}
	if args.Limit != nil {
		query = append(query, "limit="+strconv.Itoa(*args.Limit))
	}
	if len(query) > 0 {
		endpoint += "?" + strings.Join(query, "&")
	}

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
		// Do not include the endpoint in the error as it may contain query params
		// that could leak context; the URL itself has no token but keep it clean.
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

	result := watchEventsResponse{
		Events: payload.Events,
		Cursor: payload.Cursor,
		Count:  len(payload.Events),
	}

	// Ensure a nil slice is serialised as an empty array, not null.
	if result.Events == nil {
		result.Events = []json.RawMessage{}
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling events response: %w", err)
	}

	return text(string(out)), nil
}
