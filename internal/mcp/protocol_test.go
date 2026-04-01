package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_health_status","arguments":{}}}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if req.Method != "tools/call" {
		t.Fatalf("Method = %q, want %q", req.Method, "tools/call")
	}
	if req.ID == nil {
		t.Fatal("expected non-nil ID")
	}
}

func TestParseNotification(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if req.Method != "notifications/initialized" {
		t.Fatalf("Method = %q, want %q", req.Method, "notifications/initialized")
	}
	if req.ID != nil {
		t.Fatalf("expected nil ID for notification, got %s", req.ID)
	}
}

func TestToolRegistration(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	def := ToolDef{
		Name:        "my_tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
	s.RegisterTool(def, func(args json.RawMessage) (any, error) {
		return "ok", nil
	})

	tools := s.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "my_tool" {
		t.Fatalf("Name = %q, want %q", tools[0].Name, "my_tool")
	}
}

func TestToolCall(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	s.RegisterTool(ToolDef{
		Name:        "echo",
		Description: "echoes input",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(args json.RawMessage) (any, error) {
		return map[string]string{"echo": "hello"}, nil
	})

	result, err := s.CallTool("echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string result, got %T", result)
	}
	if m["echo"] != "hello" {
		t.Fatalf("echo = %q, want %q", m["echo"], "hello")
	}
}

func TestCallUnknownTool(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	_, err := s.CallTool("nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestResponseJSON(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  json.RawMessage(`{"tools":[]}`),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON output")
	}
	if !strings.Contains(string(data), `"jsonrpc"`) {
		t.Fatalf("output missing jsonrpc field: %s", data)
	}
}

func TestRunInitialize(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out strings.Builder
	if err := s.Run(in, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	resp := out.String()
	if !strings.Contains(resp, `"protocolVersion"`) {
		t.Fatalf("expected protocolVersion in response, got: %s", resp)
	}
	if !strings.Contains(resp, "2024-11-05") {
		t.Fatalf("expected protocol version 2024-11-05, got: %s", resp)
	}
}

func TestRunToolsList(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	s.RegisterTool(ToolDef{
		Name:        "my_tool",
		Description: "desc",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(args json.RawMessage) (any, error) { return nil, nil })

	in := strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out strings.Builder
	if err := s.Run(in, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	resp := out.String()
	if !strings.Contains(resp, "my_tool") {
		t.Fatalf("expected my_tool in tools/list response, got: %s", resp)
	}
}

func TestRunUnknownMethod(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	in := strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"unknown/method"}` + "\n")
	var out strings.Builder
	if err := s.Run(in, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	resp := out.String()
	if !strings.Contains(resp, `"error"`) {
		t.Fatalf("expected error in response for unknown method, got: %s", resp)
	}
	if !strings.Contains(resp, "-32601") {
		t.Fatalf("expected error code -32601, got: %s", resp)
	}
}

func TestRunNotificationNoResponse(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var out strings.Builder
	if err := s.Run(in, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Notifications should produce no output
	if out.Len() != 0 {
		t.Fatalf("expected no output for notification, got: %s", out.String())
	}
}
