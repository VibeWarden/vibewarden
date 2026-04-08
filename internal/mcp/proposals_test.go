package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startFakeAdminServer creates a test HTTP server that simulates the VibeWarden
// admin API proposals endpoint.
func startFakeAdminServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPProposalTools_RegisteredInDefaultTools(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	resp := sendLine(t, s, req)
	if resp == nil {
		t.Fatal("nil response from tools/list")
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", resp["result"])
	}
	toolsRaw, _ := result["tools"].([]any)

	names := make(map[string]bool)
	for _, tRaw := range toolsRaw {
		if tMap, ok := tRaw.(map[string]any); ok {
			if name, ok := tMap["name"].(string); ok {
				names[name] = true
			}
		}
	}

	wantTools := []string{
		"vibewarden_propose_action",
		"vibewarden_list_proposals",
		"vibewarden_get_proposal",
	}
	for _, name := range wantTools {
		if !names[name] {
			t.Errorf("tool %q not registered; found names: %v", name, names)
		}
	}
}

func TestMCPProposeAction_MissingURL(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"vibewarden_propose_action","arguments":{"admin_token":"tok","action_type":"block_ip","params":{"ip":"1.2.3.4"},"reason":"test"}}}`
	resp := sendLine(t, s, req)

	if isToolError(resp) || resp["error"] != nil {
		return // expected
	}
	t.Error("expected error when url is missing, got success response")
}

func TestMCPProposeAction_MissingAdminToken(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"vibewarden_propose_action","arguments":{"url":"http://localhost:8443","action_type":"block_ip","params":{"ip":"1.2.3.4"},"reason":"test"}}}`
	resp := sendLine(t, s, req)

	if isToolError(resp) || resp["error"] != nil {
		return // expected
	}
	t.Error("expected error when admin_token is missing, got success response")
}

func TestMCPProposeAction_UnreachableSidecar(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"vibewarden_propose_action","arguments":{"url":"http://127.0.0.1:1","admin_token":"tok","action_type":"block_ip","params":{"ip":"1.2.3.4"},"reason":"test"}}}`
	resp := sendLine(t, s, req)
	if resp == nil {
		t.Fatal("nil response")
	}

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("result is nil")
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Error("expected fallback text message for unreachable sidecar")
	}
}

func TestMCPListProposals_MissingURL(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"vibewarden_list_proposals","arguments":{"admin_token":"tok"}}}`
	resp := sendLine(t, s, req)

	if isToolError(resp) || resp["error"] != nil {
		return // expected
	}
	t.Error("expected error when url is missing")
}

func TestMCPGetProposal_MissingProposalID(t *testing.T) {
	s := newTestServer()
	RegisterDefaultTools(s)

	req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"vibewarden_get_proposal","arguments":{"url":"http://localhost:8443","admin_token":"tok"}}}`
	resp := sendLine(t, s, req)

	if isToolError(resp) || resp["error"] != nil {
		return // expected
	}
	t.Error("expected error when proposal_id is missing")
}

func TestMCPProposeAction_WithFakeServer(t *testing.T) {
	srv := startFakeAdminServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_vibewarden/admin/proposals" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "proposal-abc",
			"type":       "block_ip",
			"status":     "pending",
			"reason":     "test",
			"created_at": "2026-01-01T00:00:00Z",
			"expires_at": "2026-01-01T01:00:00Z",
			"source":     "mcp_agent",
		})
	})

	s := newTestServer()
	RegisterDefaultTools(s)

	args := map[string]any{
		"url":         srv.URL,
		"admin_token": "secret-token",
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.2.3.4"},
		"reason":      "suspicious traffic",
	}
	argsJSON, _ := json.Marshal(args)

	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"vibewarden_propose_action","arguments":` + string(argsJSON) + `}}`
	resp := sendLine(t, s, req)
	if resp == nil {
		t.Fatal("nil response")
	}
	if isToolError(resp) {
		t.Fatalf("unexpected tool error: %v", resp)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", resp["result"])
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in response")
	}
	first, _ := content[0].(map[string]any)
	textVal, _ := first["text"].(string)
	if textVal == "" {
		t.Error("expected non-empty text content")
	}

	var proposal map[string]any
	if err := json.Unmarshal([]byte(textVal), &proposal); err != nil {
		t.Fatalf("response is not valid JSON: %v\ntext: %s", err, textVal)
	}
	if proposal["id"] != "proposal-abc" {
		t.Errorf("proposal id = %v, want %q", proposal["id"], "proposal-abc")
	}
}

func TestMCPListProposals_WithFakeServer(t *testing.T) {
	srv := startFakeAdminServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_vibewarden/admin/proposals" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proposals": []map[string]any{
				{"id": "p1", "type": "block_ip", "status": "pending"},
			},
		})
	})

	s := newTestServer()
	RegisterDefaultTools(s)

	args := map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
	}
	argsJSON, _ := json.Marshal(args)

	req := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"vibewarden_list_proposals","arguments":` + string(argsJSON) + `}}`
	resp := sendLine(t, s, req)
	if resp == nil {
		t.Fatal("nil response")
	}
	if isToolError(resp) {
		t.Fatalf("unexpected tool error: %v", resp)
	}

	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content")
	}
	first, _ := content[0].(map[string]any)
	textVal, _ := first["text"].(string)

	var listResp map[string]any
	if err := json.Unmarshal([]byte(textVal), &listResp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	proposals, _ := listResp["proposals"].([]any)
	if len(proposals) != 1 {
		t.Errorf("proposals count = %d, want 1", len(proposals))
	}
}

func TestMCPGetProposal_WithFakeServer(t *testing.T) {
	srv := startFakeAdminServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_vibewarden/admin/proposals/my-proposal" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "my-proposal",
			"type":   "block_ip",
			"status": "pending",
		})
	})

	s := newTestServer()
	RegisterDefaultTools(s)

	args := map[string]any{
		"url":         srv.URL,
		"admin_token": "tok",
		"proposal_id": "my-proposal",
	}
	argsJSON, _ := json.Marshal(args)

	req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"vibewarden_get_proposal","arguments":` + string(argsJSON) + `}}`
	resp := sendLine(t, s, req)
	if resp == nil {
		t.Fatal("nil response")
	}
	if isToolError(resp) {
		t.Fatalf("unexpected tool error: %v", resp)
	}

	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content")
	}
	first, _ := content[0].(map[string]any)
	textVal, _ := first["text"].(string)

	var p map[string]any
	if err := json.Unmarshal([]byte(textVal), &p); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if p["id"] != "my-proposal" {
		t.Errorf("id = %v, want %q", p["id"], "my-proposal")
	}
}

// isToolError reports whether the JSON-RPC response contains an MCP tool error
// (isError: true in the result) or a JSON-RPC protocol error.
func isToolError(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	if resp["error"] != nil {
		return true
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return false
	}
	isErr, _ := result["isError"].(bool)
	return isErr
}
