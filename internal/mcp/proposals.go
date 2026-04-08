package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RegisterProposalTools registers the proposal-related MCP tools onto s.
func RegisterProposalTools(s *Server) {
	s.RegisterTool(proposeActionToolDef(), handleProposeAction)
	s.RegisterTool(listProposalsToolDef(), handleListProposals)
	s.RegisterTool(getProposalToolDef(), handleGetProposal)
}

// ---------------------------------------------------------------------------
// vibewarden_propose_action
// ---------------------------------------------------------------------------

func proposeActionToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_propose_action",
		Description: "Propose a configuration change to the VibeWarden sidecar. " +
			"The change is NOT applied immediately — a human must approve it via the admin API or " +
			"by calling vibewarden_propose_action and then visiting the proposals endpoint. " +
			"Returns a proposal_id and a diff preview of what would change.",
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
				"action_type": {
					Type:        "string",
					Description: "Kind of change to propose: 'block_ip', 'adjust_rate_limit', or 'update_config'.",
				},
				"params": {
					Type: "object",
					Description: "Action-specific parameters. " +
						"block_ip: {\"ip\": \"1.2.3.4\"}. " +
						"adjust_rate_limit: {\"requests_per_second\": 5, \"burst\": 10}. " +
						"update_config: arbitrary JSON merge-patch applied to the top-level config.",
				},
				"reason": {
					Type:        "string",
					Description: "Human-readable justification for why this change is being proposed.",
				},
			},
			Required: []string{"url", "admin_token", "action_type", "params", "reason"},
		},
	}
}

// proposeActionArgs holds the arguments for vibewarden_propose_action.
type proposeActionArgs struct {
	URL        string         `json:"url"`
	AdminToken string         `json:"admin_token"`
	ActionType string         `json:"action_type"`
	Params     map[string]any `json:"params"`
	Reason     string         `json:"reason"`
}

// proposalCreateRequest mirrors the JSON body expected by POST /admin/proposals.
type proposalCreateRequest struct {
	ActionType string         `json:"action_type"`
	Params     map[string]any `json:"params"`
	Reason     string         `json:"reason"`
}

func handleProposeAction(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args proposeActionArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if args.AdminToken == "" {
		return nil, fmt.Errorf("admin_token is required")
	}
	if args.ActionType == "" {
		return nil, fmt.Errorf("action_type is required")
	}
	if args.Reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	body := proposalCreateRequest{
		ActionType: args.ActionType,
		Params:     args.Params,
		Reason:     args.Reason,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/proposals"

	resp, err := adminPost(ctx, endpoint, args.AdminToken, bodyBytes)
	if err != nil {
		return text("Cannot reach the sidecar admin API. Ensure the sidecar is running and the url is correct."), nil
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return text("Authentication failed — check admin token."), nil
	case http.StatusBadRequest:
		return decodeAndFormat(resp, "Proposal rejected (bad request)")
	}

	if resp.StatusCode != http.StatusCreated {
		return text(fmt.Sprintf("Unexpected status %d from sidecar admin API.", resp.StatusCode)), nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return text("Proposal created but failed to decode response."), nil
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling proposal response: %w", err)
	}

	return text(string(out)), nil
}

// ---------------------------------------------------------------------------
// vibewarden_list_proposals
// ---------------------------------------------------------------------------

func listProposalsToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_list_proposals",
		Description: "List configuration-change proposals on the VibeWarden sidecar. " +
			"Filter by status to see only pending proposals awaiting human approval.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"url": {
					Type:        "string",
					Description: "Base URL of the VibeWarden sidecar, e.g. 'https://localhost:8443'.",
				},
				"admin_token": {
					Type:        "string",
					Description: "Bearer token for the admin API.",
				},
				"status": {
					Type:        "string",
					Description: "Filter by status: 'pending', 'approved', 'dismissed', 'expired'. Omit to return all.",
				},
			},
			Required: []string{"url", "admin_token"},
		},
	}
}

// listProposalsArgs holds the arguments for vibewarden_list_proposals.
type listProposalsArgs struct {
	URL        string `json:"url"`
	AdminToken string `json:"admin_token"`
	Status     string `json:"status"`
}

func handleListProposals(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args listProposalsArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if args.AdminToken == "" {
		return nil, fmt.Errorf("admin_token is required")
	}

	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/proposals"
	if args.Status != "" {
		endpoint += "?status=" + args.Status
	}

	resp, err := adminGet(ctx, endpoint, args.AdminToken)
	if err != nil {
		return text("Cannot reach the sidecar admin API. Ensure the sidecar is running and the url is correct."), nil
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return text("Authentication failed — check admin token."), nil
	case http.StatusBadRequest:
		return decodeAndFormat(resp, "Request rejected (bad request)")
	}

	if resp.StatusCode != http.StatusOK {
		return text(fmt.Sprintf("Unexpected status %d from sidecar admin API.", resp.StatusCode)), nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return text("Failed to decode proposals response."), nil
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling proposals response: %w", err)
	}

	return text(string(out)), nil
}

// ---------------------------------------------------------------------------
// vibewarden_get_proposal
// ---------------------------------------------------------------------------

func getProposalToolDef() ToolDefinition {
	return ToolDefinition{
		Name: "vibewarden_get_proposal",
		Description: "Retrieve a single configuration-change proposal by ID, including its full diff preview. " +
			"Use this to review a proposal before deciding to approve or dismiss it.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"url": {
					Type:        "string",
					Description: "Base URL of the VibeWarden sidecar, e.g. 'https://localhost:8443'.",
				},
				"admin_token": {
					Type:        "string",
					Description: "Bearer token for the admin API.",
				},
				"proposal_id": {
					Type:        "string",
					Description: "UUID of the proposal to retrieve.",
				},
			},
			Required: []string{"url", "admin_token", "proposal_id"},
		},
	}
}

// getProposalArgs holds the arguments for vibewarden_get_proposal.
type getProposalArgs struct {
	URL        string `json:"url"`
	AdminToken string `json:"admin_token"`
	ProposalID string `json:"proposal_id"`
}

func handleGetProposal(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args getProposalArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if args.AdminToken == "" {
		return nil, fmt.Errorf("admin_token is required")
	}
	if args.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}

	endpoint := strings.TrimRight(args.URL, "/") + "/_vibewarden/admin/proposals/" + args.ProposalID

	resp, err := adminGet(ctx, endpoint, args.AdminToken)
	if err != nil {
		return text("Cannot reach the sidecar admin API. Ensure the sidecar is running and the url is correct."), nil
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return text("Authentication failed — check admin token."), nil
	case http.StatusNotFound:
		return text(fmt.Sprintf("Proposal %q not found.", args.ProposalID)), nil
	}

	if resp.StatusCode != http.StatusOK {
		return text(fmt.Sprintf("Unexpected status %d from sidecar admin API.", resp.StatusCode)), nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return text("Failed to decode proposal response."), nil
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling proposal response: %w", err)
	}

	return text(string(out)), nil
}

// ---------------------------------------------------------------------------
// shared HTTP helpers
// ---------------------------------------------------------------------------

// adminHTTPClient returns an HTTP client configured to skip TLS verification
// for self-signed certificates (common in local VibeWarden deployments).
func adminHTTPClient() *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for local sidecar
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}
}

// adminGet performs an authenticated GET to the admin API.
// The admin_token is sent as a Bearer token and is never logged or included
// in error messages.
func adminGet(ctx context.Context, endpoint, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return adminHTTPClient().Do(req)
}

// adminPost performs an authenticated POST to the admin API with a JSON body.
// The admin_token is sent as a Bearer token and is never logged or included
// in error messages.
func adminPost(ctx context.Context, endpoint, token string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return adminHTTPClient().Do(req)
}

// decodeAndFormat attempts to decode a JSON error response and returns it as text.
// Falls back to the fallback message when decoding fails.
func decodeAndFormat(resp *http.Response, fallback string) ([]ContentItem, error) {
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return text(fallback), nil
	}
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return text(fallback), nil
	}
	return text(string(out)), nil
}
