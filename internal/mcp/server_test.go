package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// newTestServer builds a Server with a no-op logger suitable for unit tests.
func newTestServer() *Server {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	s := NewServer("vibewarden", "0.0.1-test", logger)
	return s
}

// sendLine writes a JSON-RPC request line to the server and collects the
// single response line written to out.
func sendLine(t *testing.T, srv *Server, reqJSON string) map[string]any {
	t.Helper()

	in := strings.NewReader(reqJSON + "\n")
	var out bytes.Buffer

	// Serve returns on EOF when the reader is exhausted.
	_ = srv.Serve(context.Background(), in, &out)

	line := strings.TrimSpace(out.String())
	if line == "" {
		return nil
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %q)", err, line)
	}
	return resp
}

func TestServer_Initialize(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`
	resp := sendLine(t, srv, req)

	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", resp["result"])
	}

	if got := result["protocolVersion"]; got != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %v", got, ProtocolVersion)
	}

	info, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo is not an object: %T", result["serverInfo"])
	}
	if info["name"] != "vibewarden" {
		t.Errorf("serverInfo.name = %v, want vibewarden", info["name"])
	}
}

func TestServer_ToolsList(t *testing.T) {
	srv := newTestServer()
	// Register a dummy tool.
	srv.RegisterTool(ToolDefinition{
		Name:        "dummy_tool",
		Description: "A dummy tool for testing",
		InputSchema: InputSchema{Type: "object"},
	}, func(_ context.Context, _ json.RawMessage) ([]ContentItem, error) {
		return text("dummy"), nil
	})

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	resp := sendLine(t, srv, req)

	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools is not an array: %T", result["tools"])
	}
	if len(tools) != 1 {
		t.Errorf("want 1 tool, got %d", len(tools))
	}
}

func TestServer_ToolsCall(t *testing.T) {
	srv := newTestServer()
	srv.RegisterTool(ToolDefinition{
		Name:        "echo_tool",
		Description: "Echo the message argument",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"message": {Type: "string", Description: "The message to echo"},
			},
			Required: []string{"message"},
		},
	}, func(_ context.Context, params json.RawMessage) ([]ContentItem, error) {
		var args struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}
		return text("echo: " + args.Message), nil
	})

	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo_tool","arguments":{"message":"hello"}}}`
	resp := sendLine(t, srv, req)

	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object")
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content is empty or wrong type")
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object")
	}
	if item["text"] != "echo: hello" {
		t.Errorf("text = %v, want %q", item["text"], "echo: hello")
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":4,"method":"unknown/method"}`
	resp := sendLine(t, srv, req)

	if resp["error"] == nil {
		t.Fatal("expected an error response")
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is not an object")
	}
	if code := errObj["code"]; code != float64(errCodeMethodNotFound) {
		t.Errorf("error code = %v, want %d", code, errCodeMethodNotFound)
	}
}

func TestServer_UnknownTool(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	resp := sendLine(t, srv, req)

	if resp["error"] == nil {
		t.Fatal("expected an error response for unknown tool")
	}
}

func TestServer_ParseError(t *testing.T) {
	srv := newTestServer()

	req := `{not valid json`
	resp := sendLine(t, srv, req)

	if resp == nil {
		t.Fatal("expected a parse error response")
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is not an object")
	}
	if code := errObj["code"]; code != float64(errCodeParse) {
		t.Errorf("error code = %v, want %d", code, errCodeParse)
	}
}

func TestServer_Notification(t *testing.T) {
	srv := newTestServer()
	// Notifications have no id and must not produce a response.
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var out bytes.Buffer
	_ = srv.Serve(context.Background(), in, &out)
	if out.Len() != 0 {
		t.Errorf("expected no response for notification, got %q", out.String())
	}
}

func TestServer_EmptyLine(t *testing.T) {
	srv := newTestServer()
	// Empty lines should be skipped without error or response.
	in := strings.NewReader("\n\n")
	var out bytes.Buffer
	_ = srv.Serve(context.Background(), in, &out)
	if out.Len() != 0 {
		t.Errorf("expected no response for empty lines, got %q", out.String())
	}
}

func TestServer_MultipleRequests(t *testing.T) {
	srv := newTestServer()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	in := strings.NewReader(input)
	var out bytes.Buffer
	_ = srv.Serve(context.Background(), in, &out)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 response lines, got %d: %q", len(lines), out.String())
	}
}
