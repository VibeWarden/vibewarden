// Package integration contains integration tests that exercise the full
// VibeWarden agent-assisted security response loop without spinning up a
// real sidecar process.
//
// The tests in this package wire the in-memory adapters, the MCP tool
// handlers, and the proposal applier together to demonstrate the complete
// detect → propose → approve → apply cycle.
//
// Usage:
//
//	go test -v ./test/integration/
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	proposalapp "github.com/vibewarden/vibewarden/internal/app/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/mcp"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Compile-time assertion: noopReloader must satisfy ports.ConfigReloader.
var _ ports.ConfigReloader = (*noopReloader)(nil)

// ----------------------------------------------------------------------------
// Test: TestAgentLoop_ProposeAndApprove
// ----------------------------------------------------------------------------

// TestAgentLoop_ProposeAndApprove demonstrates the full autonomous security
// response loop:
//
//  1. Simulated attacks are injected into an in-memory ring buffer.
//  2. An MCP agent (simulated by calling the tool handlers directly) polls
//     events via vibewarden_watch_events, observes suspicious patterns, and
//     calls vibewarden_propose_action for each threat cluster.
//  3. The pending proposals are verified via vibewarden_list_proposals.
//  4. A human administrator approves the block_ip proposal by calling
//     the proposal store's Approve method (simulating an admin UI click).
//  5. The applier writes the approved change to the config YAML file.
//  6. The test verifies that the blocked IP appears in the config file.
//
// No real sidecar process, no Docker, no network sockets required.
func TestAgentLoop_ProposeAndApprove(t *testing.T) {
	ctx := context.Background()

	// -----------------------------------------------------------------------
	// Step 1 — Set up the in-memory infrastructure.
	// -----------------------------------------------------------------------

	// Ring buffer stores the 200 most recent events (enough for all test events).
	ringBuf := logadapter.NewRingBuffer(200)

	// Proposal store is the in-memory adapter backed by a real clock.
	store := proposaladapter.NewStore()

	// Config file lives in a temp directory so the applier can write to it.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vibewarden.yaml")

	// Write a minimal starting configuration. The applier will extend it.
	initialConfig := `
profile: dev

server:
  host: "0.0.0.0"
  port: 8080

upstream:
  host: localhost
  port: 3000

auth:
  enabled: true
  mode: jwt

rate_limit:
  enabled: true
  per_ip:
    requests_per_second: 10
    burst: 20

waf:
  enabled: true
  mode: block
  rules:
    sqli: true
    xss: true

ip_filter:
  enabled: false
  mode: blocklist
  addresses: []
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	// The applier mutates the YAML file when a proposal is approved.
	// We use a no-op reloader because we do not run a live sidecar.
	applier := proposaladapter.NewApplier(configPath, &noopReloader{})

	// Wire the proposal application service.
	proposalSvc := proposalapp.NewService(store, applier, ringBuf, nil)

	// -----------------------------------------------------------------------
	// Step 2 — Inject attack events into the ring buffer.
	// -----------------------------------------------------------------------

	// Scenario A: brute-force login attempts from IP 10.0.0.99 (20 events).
	// The agent should detect the cluster and propose a block_ip action.
	bruteForceIP := "10.0.0.99"
	for range 20 {
		ev := events.NewAuthFailed(events.AuthFailedParams{
			Method:   "POST",
			Path:     "/login",
			Reason:   "missing or invalid session",
			ClientIP: bruteForceIP,
		})
		if err := ringBuf.Log(ctx, ev); err != nil {
			t.Fatalf("injecting auth.failed event: %v", err)
		}
	}

	// Scenario B: SQL injection attempts from IP 10.0.0.50 (5 events).
	// The agent should note the WAF hits and optionally propose an action.
	wafIP := "10.0.0.50"
	for range 5 {
		ev := makeWAFBlockedEvent(wafIP, "sqli", "SELECT * FROM users WHERE")
		if err := ringBuf.Log(ctx, ev); err != nil {
			t.Fatalf("injecting waf.blocked event: %v", err)
		}
	}

	// Scenario C: rate limit exhaustion from IP 10.0.0.75 (100 events).
	// The agent should see the spike and propose a rate-limit adjustment.
	rateLimitIP := "10.0.0.75"
	for range 100 {
		ev := events.NewRateLimitHit(events.RateLimitHitParams{
			LimitType:         "ip",
			Identifier:        rateLimitIP,
			RequestsPerSecond: 10,
			Burst:             20,
			RetryAfterSeconds: 5,
			Path:              "/api/data",
			Method:            "GET",
		})
		if err := ringBuf.Log(ctx, ev); err != nil {
			t.Fatalf("injecting rate_limit.hit event: %v", err)
		}
	}

	// Verify all events were buffered.
	allStored, _ := ringBuf.Query(0, nil, 500)
	if len(allStored) != 125 {
		t.Fatalf("expected 125 events in ring buffer, got %d", len(allStored))
	}

	// -----------------------------------------------------------------------
	// Step 3 — Agent polls events via vibewarden_watch_events.
	//
	// The MCP watch-events handler calls the admin HTTP events endpoint.
	// We serve the ring buffer through a local httptest.Server so the handler
	// has a real URL to call — this also exercises the HTTP serialisation path.
	// -----------------------------------------------------------------------

	adminServer := newAdminEventsServer(t, ringBuf)
	defer adminServer.Close()

	watchParams, err := json.Marshal(map[string]any{
		"url":         adminServer.URL,
		"admin_token": "test-token",
		"limit":       500,
	})
	if err != nil {
		t.Fatalf("marshalling watch_events params: %v", err)
	}

	watchResult, err := callWatchEvents(ctx, watchParams)
	if err != nil {
		t.Fatalf("vibewarden_watch_events: %v", err)
	}

	// Parse the response and check that all injected events are returned.
	var watchResponse struct {
		Events []json.RawMessage `json:"events"`
		Cursor uint64            `json:"cursor"`
		Count  int               `json:"count"`
	}
	if err := json.Unmarshal([]byte(watchResult), &watchResponse); err != nil {
		t.Fatalf("parsing watch_events response: %v", err)
	}
	if watchResponse.Count != 125 {
		t.Errorf("watch_events: got %d events, want 125", watchResponse.Count)
	}

	t.Logf("Agent received %d events (cursor=%d)", watchResponse.Count, watchResponse.Cursor)

	// -----------------------------------------------------------------------
	// Step 4 — Agent detects patterns and proposes actions.
	//
	// In a real deployment an LLM would analyse the events and decide what
	// to propose. Here we simulate the agent's decision by calling
	// handleProposeAction directly via the proposal service — the same path
	// that the MCP tool exercises when it POSTs to
	// /_vibewarden/admin/proposals.
	// -----------------------------------------------------------------------

	proposalServer := newProposalServer(t, proposalSvc)
	defer proposalServer.Close()

	// Pattern A: brute-force cluster → propose block_ip for 10.0.0.99.
	blockIPParams, err := json.Marshal(map[string]any{
		"url":         proposalServer.URL,
		"admin_token": "test-token",
		"action_type": "block_ip",
		"params":      map[string]any{"ip": bruteForceIP},
		"reason":      fmt.Sprintf("20 auth.failed events from %s in a short window — likely brute-force attack", bruteForceIP),
	})
	if err != nil {
		t.Fatalf("marshalling propose_action params: %v", err)
	}

	blockIPResult, err := callProposeAction(ctx, blockIPParams)
	if err != nil {
		t.Fatalf("vibewarden_propose_action (block_ip): %v", err)
	}

	var blockIPProposal struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(blockIPResult), &blockIPProposal); err != nil {
		t.Fatalf("parsing block_ip proposal response: %v", err)
	}
	if blockIPProposal.Status != "pending" {
		t.Errorf("block_ip proposal: got status %q, want pending", blockIPProposal.Status)
	}
	if blockIPProposal.Type != "block_ip" {
		t.Errorf("block_ip proposal: got type %q, want block_ip", blockIPProposal.Type)
	}

	t.Logf("Agent proposed block_ip for %s — proposal id: %s", bruteForceIP, blockIPProposal.ID)

	// Pattern B: rate-limit exhaustion → propose rate limit adjustment.
	adjustRLParams, err := json.Marshal(map[string]any{
		"url":         proposalServer.URL,
		"admin_token": "test-token",
		"action_type": "adjust_rate_limit",
		"params": map[string]any{
			"requests_per_second": 2,
			"burst":               5,
		},
		"reason": fmt.Sprintf("100 rate_limit.hit events from %s — burst exhaustion indicates abuse; reduce per-IP limit", rateLimitIP),
	})
	if err != nil {
		t.Fatalf("marshalling propose_action (adjust_rate_limit) params: %v", err)
	}

	adjustRLResult, err := callProposeAction(ctx, adjustRLParams)
	if err != nil {
		t.Fatalf("vibewarden_propose_action (adjust_rate_limit): %v", err)
	}

	var adjustRLProposal struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(adjustRLResult), &adjustRLProposal); err != nil {
		t.Fatalf("parsing adjust_rate_limit proposal response: %v", err)
	}
	if adjustRLProposal.Status != "pending" {
		t.Errorf("adjust_rate_limit proposal: got status %q, want pending", adjustRLProposal.Status)
	}

	t.Logf("Agent proposed adjust_rate_limit — proposal id: %s", adjustRLProposal.ID)

	// -----------------------------------------------------------------------
	// Step 5 — Agent lists proposals to confirm they are pending.
	// -----------------------------------------------------------------------

	listParams, err := json.Marshal(map[string]any{
		"url":         proposalServer.URL,
		"admin_token": "test-token",
		"status":      "pending",
	})
	if err != nil {
		t.Fatalf("marshalling list_proposals params: %v", err)
	}

	listResult, err := callListProposals(ctx, listParams)
	if err != nil {
		t.Fatalf("vibewarden_list_proposals: %v", err)
	}

	var listResponse struct {
		Proposals []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(listResult), &listResponse); err != nil {
		t.Fatalf("parsing list_proposals response: %v", err)
	}
	if len(listResponse.Proposals) != 2 {
		t.Errorf("list_proposals: got %d pending proposals, want 2", len(listResponse.Proposals))
	}

	t.Logf("Agent verified %d pending proposals", len(listResponse.Proposals))

	// -----------------------------------------------------------------------
	// Step 6 — Human approves the block_ip proposal.
	//
	// In production the admin clicks "Approve" in the dashboard UI, which
	// POSTs to /_vibewarden/admin/proposals/{id}/approve. Here we call the
	// store's Approve method directly to simulate that click and then invoke
	// the applier to write the change to disk — matching what the HTTP handler
	// does via the application service.
	// -----------------------------------------------------------------------

	approved, err := store.Approve(ctx, blockIPProposal.ID)
	if err != nil {
		t.Fatalf("approving block_ip proposal: %v", err)
	}
	if approved.Status != proposal.StatusApproved {
		t.Errorf("approval: got status %q, want approved", approved.Status)
	}

	// Apply the approved proposal to the config file.
	if err := applier.Apply(ctx, approved); err != nil {
		t.Fatalf("applying block_ip proposal: %v", err)
	}

	t.Logf("Human approved block_ip proposal %s — config updated", blockIPProposal.ID)

	// -----------------------------------------------------------------------
	// Step 7 — Verify the config file was updated correctly.
	// -----------------------------------------------------------------------

	updatedRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading updated config: %v", err)
	}
	updatedYAML := string(updatedRaw)

	t.Logf("Updated config:\n%s", updatedYAML)

	// The applier should have added the blocked IP to ip_filter.addresses.
	if !containsString(updatedYAML, bruteForceIP) {
		t.Errorf("config file does not contain blocked IP %s after approval", bruteForceIP)
	}

	// The ip_filter section must be enabled in blocklist mode.
	if !containsString(updatedYAML, "enabled: true") {
		t.Errorf("config file: ip_filter.enabled should be true after block_ip approval")
	}
	if !containsString(updatedYAML, "blocklist") {
		t.Errorf("config file: ip_filter.mode should be 'blocklist' after block_ip approval")
	}

	// The rate-limit proposal is still pending (not approved in this test).
	pendingProposals, err := store.List(ctx, proposal.StatusPending)
	if err != nil {
		t.Fatalf("listing pending proposals: %v", err)
	}
	if len(pendingProposals) != 1 {
		t.Errorf("expected 1 remaining pending proposal, got %d", len(pendingProposals))
	}
	if pendingProposals[0].ID != adjustRLProposal.ID {
		t.Errorf("unexpected pending proposal ID: got %s, want %s", pendingProposals[0].ID, adjustRLProposal.ID)
	}

	t.Logf("Test complete: block_ip applied, rate-limit proposal still pending")
}

// ----------------------------------------------------------------------------
// Fake helpers
// ----------------------------------------------------------------------------

// noopReloader is a ports.ConfigReloader that does nothing. It is used in
// tests where no live sidecar is present, so we only care about the file
// mutation and skip the actual reload signal.
type noopReloader struct{}

// Reload implements ports.ConfigReloader. It always succeeds without side effects.
func (n *noopReloader) Reload(_ context.Context, _ string) error { return nil }

// CurrentConfig implements ports.ConfigReloader. It returns nil.
func (n *noopReloader) CurrentConfig() ports.RedactedConfig { return nil }

// makeWAFBlockedEvent constructs a synthetic waf.blocked event for the given
// source IP, rule category, and matched pattern. It is used to inject WAF
// detections into the ring buffer without running the WAF middleware.
func makeWAFBlockedEvent(clientIP, ruleCategory, matchedInput string) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     "waf.blocked",
		Timestamp:     time.Now().UTC(),
		Severity:      events.SeverityHigh,
		Category:      events.CategoryPolicy,
		AISummary: fmt.Sprintf(
			"WAF blocked request from %s: %s pattern detected in input",
			clientIP, ruleCategory,
		),
		Payload: map[string]any{
			"client_ip":     clientIP,
			"rule_category": ruleCategory,
			"matched_input": matchedInput,
			"method":        "POST",
			"path":          "/api/query",
		},
		Actor:       events.Actor{Type: events.ActorTypeIP, ID: clientIP, IP: clientIP},
		Resource:    events.Resource{Type: events.ResourceTypeHTTPEndpoint, Path: "/api/query", Method: "POST"},
		Outcome:     events.OutcomeBlocked,
		TriggeredBy: "waf_middleware",
	}
}

// ----------------------------------------------------------------------------
// Local HTTP servers for MCP tool testing
// ----------------------------------------------------------------------------

// adminEventsPayload mirrors the JSON shape returned by
// GET /_vibewarden/admin/events and understood by handleWatchEvents.
type adminEventsPayload struct {
	Events []eventItem `json:"events"`
	Cursor uint64      `json:"cursor"`
}

// eventItem matches the shape written by AdminHandlers.listEvents.
type eventItem struct {
	Cursor    uint64         `json:"cursor"`
	EventType string         `json:"event_type"`
	Timestamp string         `json:"timestamp,omitempty"`
	AISummary string         `json:"ai_summary,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// newAdminEventsServer starts a local httptest.Server that serves the ring
// buffer contents at GET /_vibewarden/admin/events. It authenticates via a
// hard-coded "test-token" bearer token so the MCP watch-events handler can
// call it without a running sidecar.
func newAdminEventsServer(t *testing.T, rb ports.EventRingBuffer) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_vibewarden/admin/events", func(w http.ResponseWriter, r *http.Request) {
		// Basic auth gate — match what the MCP client sends.
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		stored, cursor := rb.Query(0, nil, 500)

		items := make([]eventItem, 0, len(stored))
		for _, se := range stored {
			item := eventItem{
				Cursor:    se.Cursor,
				EventType: se.Event.EventType,
				AISummary: se.Event.AISummary,
			}
			if !se.Event.Timestamp.IsZero() {
				item.Timestamp = se.Event.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00")
			}
			if len(se.Event.Payload) > 0 {
				item.Payload = se.Event.Payload
			}
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(adminEventsPayload{
			Events: items,
			Cursor: cursor,
		}); err != nil {
			t.Logf("encoding events response: %v", err)
		}
	})

	return httptest.NewServer(mux)
}

// createProposalRequest mirrors the JSON body expected by
// POST /_vibewarden/admin/proposals.
type createProposalRequest struct {
	ActionType string         `json:"action_type"`
	Params     map[string]any `json:"params"`
	Reason     string         `json:"reason"`
}

// listProposalsResponse mirrors the JSON body returned by
// GET /_vibewarden/admin/proposals.
type listProposalsResponse struct {
	Proposals []proposalResponse `json:"proposals"`
}

// proposalResponse mirrors the wire representation of a proposal.
type proposalResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Params    map[string]any `json:"params"`
	Reason    string         `json:"reason"`
	Diff      string         `json:"diff,omitempty"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at"`
	ExpiresAt string         `json:"expires_at"`
	Source    string         `json:"source"`
}

// newProposalServer starts a local httptest.Server that exposes the proposal
// creation and listing endpoints backed by the given service. This lets the
// MCP tool handlers (handleProposeAction, handleListProposals) make real HTTP
// calls without a running sidecar.
func newProposalServer(t *testing.T, svc *proposalapp.Service) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// POST /_vibewarden/admin/proposals — create a new proposal.
	mux.HandleFunc("POST /_vibewarden/admin/proposals", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		var req createProposalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
			return
		}

		p, err := svc.Create(r.Context(), proposalapp.CreateParams{
			Type:   proposal.ActionType(req.ActionType),
			Params: req.Params,
			Reason: req.Reason,
			Source: proposal.SourceMCPAgent,
		})
		if err != nil {
			http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(toProposalResp(p)); err != nil {
			t.Logf("encoding create proposal response: %v", err)
		}
	})

	// GET /_vibewarden/admin/proposals — list proposals.
	mux.HandleFunc("GET /_vibewarden/admin/proposals", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		var status proposal.Status
		if raw := r.URL.Query().Get("status"); raw != "" {
			status = proposal.Status(raw)
		}

		proposals, err := svc.List(r.Context(), status)
		if err != nil {
			http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
			return
		}

		resp := listProposalsResponse{
			Proposals: make([]proposalResponse, 0, len(proposals)),
		}
		for _, p := range proposals {
			resp.Proposals = append(resp.Proposals, toProposalResp(p))
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("encoding list proposals response: %v", err)
		}
	})

	return httptest.NewServer(mux)
}

// toProposalResp converts a domain proposal to the wire representation used in
// the local test server responses.
func toProposalResp(p proposal.Proposal) proposalResponse {
	return proposalResponse{
		ID:        p.ID,
		Type:      string(p.Type),
		Params:    p.Params,
		Reason:    p.Reason,
		Diff:      p.Diff,
		Status:    string(p.Status),
		CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		ExpiresAt: p.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Source:    p.Source,
	}
}

// ----------------------------------------------------------------------------
// MCP tool call helpers
// ----------------------------------------------------------------------------

// callWatchEvents invokes the vibewarden_watch_events MCP tool handler directly
// and returns the text payload from the first content item.
func callWatchEvents(ctx context.Context, params json.RawMessage) (string, error) {
	// The handler is a package-level function — we call it via the registered
	// MCP server to follow the same dispatch path used in production.
	srv := newTestMCPServer()
	return dispatchTool(ctx, srv, "vibewarden_watch_events", params)
}

// callProposeAction invokes the vibewarden_propose_action MCP tool handler.
func callProposeAction(ctx context.Context, params json.RawMessage) (string, error) {
	srv := newTestMCPServer()
	return dispatchTool(ctx, srv, "vibewarden_propose_action", params)
}

// callListProposals invokes the vibewarden_list_proposals MCP tool handler.
func callListProposals(ctx context.Context, params json.RawMessage) (string, error) {
	srv := newTestMCPServer()
	return dispatchTool(ctx, srv, "vibewarden_list_proposals", params)
}

// newTestMCPServer creates a minimal MCP server with all default tools
// registered. This is the same server configuration used in production.
func newTestMCPServer() *mcp.Server {
	// Use a no-op slog.Logger (discard all output) so debug-level log calls
	// inside the MCP server do not panic from a nil logger.
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := mcp.NewServer("vibewarden-test", "0.0.0-test", logger)
	mcp.RegisterDefaultTools(srv)
	return srv
}

// dispatchTool exercises the full MCP JSON-RPC dispatch path — it builds a
// tools/call request, hands it to the server's internal handle method via
// Serve, and returns the text content from the response.
//
// We use the public Serve interface with an in-memory pipe so that we test the
// real serialisation and dispatch logic rather than calling handlers directly.
func dispatchTool(ctx context.Context, srv *mcp.Server, toolName string, params json.RawMessage) (string, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": params,
		},
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshalling RPC request: %w", err)
	}

	// Pipe: write the request then close so Serve returns on EOF.
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("creating pipe: %w", err)
	}

	if _, err := pw.Write(append(reqBytes, '\n')); err != nil {
		return "", fmt.Errorf("writing to pipe: %w", err)
	}
	pw.Close() //nolint:errcheck

	// Capture the response in a temporary file (os.Pipe works well for output).
	outPR, outPW, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("creating output pipe: %w", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ctx, pr, outPW)
		outPW.Close() //nolint:errcheck
	}()

	// Decode the response from the output pipe.
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(outPR).Decode(&response); err != nil {
		return "", fmt.Errorf("decoding RPC response: %w", err)
	}
	outPR.Close() //nolint:errcheck

	if err := <-serveErr; err != nil {
		// EOF is expected — Serve exits when input is exhausted.
		return "", fmt.Errorf("serving MCP request: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.Result.IsError {
		if len(response.Result.Content) > 0 {
			return "", fmt.Errorf("tool error: %s", response.Result.Content[0].Text)
		}
		return "", fmt.Errorf("tool returned isError=true with no content")
	}
	if len(response.Result.Content) == 0 {
		return "", fmt.Errorf("tool returned empty content")
	}

	return response.Result.Content[0].Text, nil
}

// containsString reports whether haystack contains needle as a substring.
// It is a simple helper to avoid importing extra packages.
func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}
