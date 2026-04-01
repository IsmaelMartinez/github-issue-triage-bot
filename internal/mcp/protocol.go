// Package mcp implements a minimal MCP (Model Context Protocol) server
// using JSON-RPC 2.0 over stdio.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDef describes a tool's metadata for the tools/list response.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolHandler is a function that handles a tool call and returns a result.
type ToolHandler func(args json.RawMessage) (any, error)

// registeredTool pairs a ToolDef with its handler.
type registeredTool struct {
	def     ToolDef
	handler ToolHandler
}

// Server is a minimal MCP server that dispatches JSON-RPC 2.0 requests over stdio.
type Server struct {
	name    string
	version string
	tools   map[string]registeredTool
	logger  *slog.Logger
}

// NewServer creates a new Server with the given name and version.
func NewServer(name, version string) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   make(map[string]registeredTool),
		logger:  slog.Default(),
	}
}

// RegisterTool adds a tool to the server's registry.
func (s *Server) RegisterTool(def ToolDef, handler ToolHandler) {
	s.tools[def.Name] = registeredTool{def: def, handler: handler}
}

// ListTools returns all registered tool definitions.
func (s *Server) ListTools() []ToolDef {
	defs := make([]ToolDef, 0, len(s.tools))
	for _, t := range s.tools {
		defs = append(defs, t.def)
	}
	return defs
}

// CallTool invokes a registered tool by name. Returns an error if the tool is unknown.
func (s *Server) CallTool(name string, args json.RawMessage) (any, error) {
	t, ok := s.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return t.handler(args)
}

// Run reads JSON-RPC messages line-by-line from in and writes responses to out.
// It returns when in reaches EOF. The scanner uses a 1 MB buffer to accommodate
// large requests.
func (s *Server) Run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))

	enc := json.NewEncoder(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.logger.Error("failed to parse request", "err", err)
			continue
		}

		// Notifications have no ID — log and skip.
		if req.ID == nil {
			s.handleNotification(req)
			continue
		}

		resp := s.handleRequest(req)
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("encode response: %w", err)
		}
	}

	return scanner.Err()
}

// handleNotification handles a JSON-RPC notification (no ID, no response sent).
func (s *Server) handleNotification(req Request) {
	s.logger.Debug("notification received", "method", req.Method)
}

// handleRequest dispatches a JSON-RPC request and returns the response.
func (s *Server) handleRequest(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// handleInitialize responds with server info and capabilities.
func (s *Server) handleInitialize(req Request) Response {
	type serverInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type capabilities struct {
		Tools map[string]any `json:"tools"`
	}
	result := struct {
		ProtocolVersion string       `json:"protocolVersion"`
		Capabilities    capabilities `json:"capabilities"`
		ServerInfo      serverInfo   `json:"serverInfo"`
	}{
		ProtocolVersion: "2024-11-05",
		Capabilities:    capabilities{Tools: map[string]any{}},
		ServerInfo:      serverInfo{Name: s.name, Version: s.version},
	}
	return marshalResult(req.ID, result)
}

// handleToolsList responds with all registered tools.
func (s *Server) handleToolsList(req Request) Response {
	result := struct {
		Tools []ToolDef `json:"tools"`
	}{Tools: s.ListTools()}
	return marshalResult(req.ID, result)
}

// handleToolsCall parses the call params, invokes the tool, and wraps the result.
func (s *Server) handleToolsCall(req Request) Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "invalid params")
	}

	toolResult, err := s.CallTool(params.Name, params.Arguments)
	if err != nil {
		return errorResponse(req.ID, -32601, err.Error())
	}

	encoded, err := json.Marshal(toolResult)
	if err != nil {
		return errorResponse(req.ID, -32603, "internal error: failed to encode tool result")
	}

	content := struct {
		Content []map[string]string `json:"content"`
	}{
		Content: []map[string]string{
			{"type": "text", "text": string(encoded)},
		},
	}
	return marshalResult(req.ID, content)
}

// marshalResult marshals result into a successful Response.
func marshalResult(id json.RawMessage, result any) Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return errorResponse(id, -32603, "internal error: failed to marshal result")
	}
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	}
}

// errorResponse builds a JSON-RPC error Response.
func errorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
