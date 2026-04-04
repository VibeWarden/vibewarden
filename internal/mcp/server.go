// Package mcp implements a Model Context Protocol (MCP) server for VibeWarden.
// The server communicates over stdio using JSON-RPC 2.0 and exposes VibeWarden
// tools that AI agents can call to inspect and validate a local sidecar.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// ProtocolVersion is the MCP protocol version this server implements.
const ProtocolVersion = "2024-11-05"

// Request is a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 2.0 error codes.
const (
	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// ToolDefinition describes an MCP tool that the server exposes.
type ToolDefinition struct {
	// Name is the tool identifier used in tools/call requests.
	Name string `json:"name"`
	// Description is a human-readable summary shown to the AI agent.
	Description string `json:"description"`
	// InputSchema is a JSON Schema describing the tool's parameters.
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is a simplified JSON Schema for tool input parameters.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a single parameter in the input schema.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ContentItem is a single item in a tool result's content array.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolHandler is a function that implements a tool.
// It receives the raw JSON params and returns content items or an error.
type ToolHandler func(ctx context.Context, params json.RawMessage) ([]ContentItem, error)

// Server is the MCP server. It reads JSON-RPC 2.0 requests from in,
// dispatches them to registered tools, and writes responses to out.
// All diagnostic output goes to logger (never to out).
type Server struct {
	name     string
	version  string
	tools    []ToolDefinition
	handlers map[string]ToolHandler
	logger   *slog.Logger
}

// NewServer creates a new Server with the given name and version.
func NewServer(name, version string, logger *slog.Logger) *Server {
	return &Server{
		name:     name,
		version:  version,
		tools:    nil,
		handlers: make(map[string]ToolHandler),
		logger:   logger,
	}
}

// RegisterTool adds a tool to the server.
// RegisterTool must be called before Serve.
func (s *Server) RegisterTool(def ToolDefinition, handler ToolHandler) {
	s.tools = append(s.tools, def)
	s.handlers[def.Name] = handler
}

// Serve reads JSON-RPC messages from in and writes responses to out.
// It blocks until in is closed or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Increase buffer for large messages (e.g. explain with a big config).
	const maxBuf = 4 * 1024 * 1024 // 4 MiB
	buf := make([]byte, maxBuf)
	scanner.Buffer(buf, maxBuf)

	encoder := json.NewEncoder(out)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			// EOF — client closed the connection.
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		resp := s.handle(ctx, line)
		if resp == nil {
			// Notification — no response required.
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			s.logger.Error("encoding response", "err", err)
		}
	}
}

// handle processes a single raw JSON message and returns the response.
// It returns nil for notifications (requests without an id).
func (s *Server) handle(ctx context.Context, raw []byte) *Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		// Cannot determine the request ID when parsing fails.
		return &Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: errCodeParse, Message: "parse error"},
		}
	}

	s.logger.Debug("received request", "method", req.Method, "id", string(req.ID))

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "notifications/initialized":
		// Notification — client signals it has processed the initialize response.
		return nil
	default:
		if req.ID == nil {
			// It's a notification — swallow silently.
			return nil
		}
		return errResp(req.ID, errCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleInitialize responds to the MCP initialize handshake.
func (s *Server) handleInitialize(req Request) *Response {
	result := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// handleToolsList responds with the list of available tools.
func (s *Server) handleToolsList(req Request) *Response {
	result := map[string]any{
		"tools": s.tools,
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// toolCallParams is the params object for a tools/call request.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolsCall dispatches a tools/call request to the appropriate handler.
func (s *Server) handleToolsCall(ctx context.Context, req Request) *Response {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, errCodeInvalidParams, "invalid params: "+err.Error())
	}

	handler, ok := s.handlers[p.Name]
	if !ok {
		return errResp(req.ID, errCodeMethodNotFound, fmt.Sprintf("unknown tool: %s", p.Name))
	}

	content, err := handler(ctx, p.Arguments)
	if err != nil {
		// Tool errors are returned as a successful JSON-RPC response that
		// contains an isError flag — this is the MCP convention.
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []ContentItem{{Type: "text", Text: err.Error()}},
				"isError": true,
			},
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": content,
		},
	}
}

// errResp constructs a JSON-RPC 2.0 error response.
func errResp(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
