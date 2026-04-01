# Triage Bot MCP Server — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the triage bot's data as a typed MCP server (stdio JSON-RPC) so Claude Code agents and other MCP clients can query pending triage, synthesis briefings, health status, and trend data.

**Architecture:** A new `cmd/mcp/main.go` binary implements the MCP protocol (JSON-RPC 2.0 over stdio) using Go's standard library. It connects to the triage bot's HTTP API for data. Before building the MCP server, Task 16a enriches `/report/trends` with synthesis findings (clusters, drift, upstream) so the MCP tools expose richer data from day one.

**Tech Stack:** Go 1.26, standard library only (encoding/json, bufio, net/http, os), existing store and HTTP endpoints.

**Spec:** `docs/superpowers/specs/2026-04-01-agentic-integration-design.md`

---

## File Structure

```
internal/mcp/                    # MCP protocol implementation (reusable)
  protocol.go                    # JSON-RPC types, message handling, server loop
  protocol_test.go               # Unit tests for protocol parsing and dispatch

internal/mcp/tools/              # MCP tool implementations (one per tool)
  pending_triage.go              # get_pending_triage tool
  pending_triage_test.go
  synthesis_briefing.go          # get_synthesis_briefing tool
  synthesis_briefing_test.go
  health_status.go               # get_health_status tool
  health_status_test.go
  report_trends.go               # get_report_trends tool
  report_trends_test.go

cmd/mcp/main.go                  # Entry point: wires tools, starts stdio server

internal/store/report.go         # Modified: add GetSynthesisFindings query (Task 16a)
internal/store/report_test.go    # Modified: add test for synthesis findings model

cmd/server/main.go               # Modified: enrich /report/trends response (Task 16a)
```

---

## Task 1: Enrich /report/trends with Synthesis Findings (Task 16a)

**Files:**
- Modify: `internal/store/report.go` — add `SynthesisFindings` type and `GetRecentFindings` query
- Modify: `internal/store/report_test.go` — add model test
- Modify: `cmd/server/main.go` — include findings in `/report/trends` response

This enriches the existing `/report/trends` endpoint so it returns recent synthesis findings alongside the existing weekly time-series data. The MCP server (Task 5) wraps this endpoint, so enriching it first means the MCP tools are richer from day one.

- [ ] **Step 1: Write failing test for SynthesisFindings model**

```go
// Add to internal/store/report_test.go
func TestSynthesisFindingsModel(t *testing.T) {
	f := SynthesisFindings{
		Clusters: []FindingSummary{
			{Title: "Audio issues cluster", Severity: "warning", Evidence: []string{"#10", "#12", "#15"}, Suggestion: "Consider roadmap item"},
		},
		Drift: []FindingSummary{
			{Title: "ADR-007 contradicted", Severity: "action_needed", Evidence: []string{"PR #50"}, Suggestion: "Revise ADR"},
		},
		Upstream: []FindingSummary{
			{Title: "Electron v40 WebRTC", Severity: "info", Evidence: []string{"electron/electron"}, Suggestion: "Check ADR-007"},
		},
	}

	if len(f.Clusters) != 1 {
		t.Errorf("Clusters length = %d, want 1", len(f.Clusters))
	}
	if f.Clusters[0].Severity != "warning" {
		t.Errorf("Clusters[0].Severity = %q, want %q", f.Clusters[0].Severity, "warning")
	}
	if len(f.Drift) != 1 {
		t.Errorf("Drift length = %d, want 1", len(f.Drift))
	}
	if len(f.Upstream) != 1 {
		t.Errorf("Upstream length = %d, want 1", len(f.Upstream))
	}
	if f.Drift[0].Title != "ADR-007 contradicted" {
		t.Errorf("Drift[0].Title = %q", f.Drift[0].Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSynthesisFindingsModel -v`
Expected: FAIL — `SynthesisFindings` and `FindingSummary` types not defined.

- [ ] **Step 3: Implement SynthesisFindings types**

Add to `internal/store/report.go` after the existing `WeeklyFeedback` type:

```go
// FindingSummary is a serialisable representation of a synthesis finding.
type FindingSummary struct {
	Title      string   `json:"title"`
	Severity   string   `json:"severity"`
	Evidence   []string `json:"evidence"`
	Suggestion string   `json:"suggestion"`
}

// SynthesisFindings groups recent synthesis findings by type.
type SynthesisFindings struct {
	Clusters []FindingSummary `json:"clusters"`
	Drift    []FindingSummary `json:"drift"`
	Upstream []FindingSummary `json:"upstream"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSynthesisFindingsModel -v`
Expected: PASS

- [ ] **Step 5: Add GetRecentFindings store method**

This queries `repo_events` for recent `briefing_posted` events and extracts findings from the event metadata. The synthesis runner already records findings count in metadata when posting briefings. However, the individual findings are not currently stored — they're only in the briefing markdown. The simplest approach is to query `repo_events` for briefing events and parse the linked shadow issue content. But that requires GitHub API calls.

A better approach: extend the runner to also store findings as structured JSON in `repo_events.metadata`. Then `GetRecentFindings` simply queries the journal.

Add to `internal/store/report.go`:

```go
// GetRecentFindings returns synthesis findings from recent briefing events.
// Findings are stored in repo_events.metadata by the synthesis runner.
func (s *Store) GetRecentFindings(ctx context.Context, repo string, since time.Time) (*SynthesisFindings, error) {
	result := &SynthesisFindings{
		Clusters: []FindingSummary{},
		Drift:    []FindingSummary{},
		Upstream: []FindingSummary{},
	}

	rows, err := s.pool.Query(ctx, `
		SELECT metadata FROM repo_events
		WHERE repo = $1 AND event_type = 'briefing_posted' AND created_at > $2
		ORDER BY created_at DESC
		LIMIT 4
	`, repo, since)
	if err != nil {
		return result, fmt.Errorf("query briefing events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var meta map[string]any
		if err := rows.Scan(&meta); err != nil {
			continue
		}
		extractFindings(meta, "clusters", &result.Clusters)
		extractFindings(meta, "drift", &result.Drift)
		extractFindings(meta, "upstream", &result.Upstream)
	}

	return result, nil
}

// extractFindings pulls typed findings from briefing event metadata.
func extractFindings(meta map[string]any, key string, out *[]FindingSummary) {
	raw, ok := meta[key]
	if !ok {
		return
	}
	items, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		f := FindingSummary{
			Title:      stringFromMap(m, "title"),
			Severity:   stringFromMap(m, "severity"),
			Suggestion: stringFromMap(m, "suggestion"),
		}
		if ev, ok := m["evidence"].([]any); ok {
			for _, e := range ev {
				if s, ok := e.(string); ok {
					f.Evidence = append(f.Evidence, s)
				}
			}
		}
		*out = append(*out, f)
	}
}

func stringFromMap(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/store/ -v`
Expected: PASS (existing tests still pass, new model test passes)

- [ ] **Step 7: Update synthesis runner to store findings in metadata**

Modify `internal/synthesis/runner.go` — in the `Run` method, change the metadata to include structured findings:

Replace the event recording block (after `issueNumber, err := r.github.CreateIssue(...)`) to include classified findings:

```go
	// Record briefing event in journal with structured findings (best-effort)
	if r.store != nil {
		classified := classifyFindings(allFindings)
		if evErr := r.store.RecordEvent(ctx, store.RepoEvent{
			Repo:      repo,
			EventType: "briefing_posted",
			SourceRef: fmt.Sprintf("#%d", issueNumber),
			Summary:   title,
			Metadata:  classified,
		}); evErr != nil {
			r.logger.Error("recording briefing event", "error", evErr)
		}
	}
```

Add the `classifyFindings` helper at the bottom of `runner.go`:

```go
// classifyFindings groups findings by type for structured storage in event metadata.
func classifyFindings(findings []Finding) map[string]any {
	clusters := []map[string]any{}
	drift := []map[string]any{}
	upstream := []map[string]any{}

	for _, f := range findings {
		entry := map[string]any{
			"title":      f.Title,
			"severity":   f.Severity,
			"evidence":   f.Evidence,
			"suggestion": f.Suggestion,
		}
		switch f.Type {
		case "cluster":
			clusters = append(clusters, entry)
		case "drift", "staleness":
			drift = append(drift, entry)
		case "upstream_signal":
			upstream = append(upstream, entry)
		}
	}

	return map[string]any{
		"findings": len(findings),
		"clusters": clusters,
		"drift":    drift,
		"upstream": upstream,
	}
}
```

- [ ] **Step 8: Add test for classifyFindings**

Add to `internal/synthesis/runner_test.go` (create if it doesn't exist):

```go
package synthesis

import "testing"

func TestClassifyFindings(t *testing.T) {
	findings := []Finding{
		{Type: "cluster", Severity: "warning", Title: "Audio cluster", Evidence: []string{"#1", "#2"}, Suggestion: "Investigate"},
		{Type: "drift", Severity: "info", Title: "ADR drift", Evidence: []string{"PR #5"}, Suggestion: "Revise"},
		{Type: "staleness", Severity: "warning", Title: "Stale roadmap", Evidence: []string{"roadmap.md"}, Suggestion: "Update"},
		{Type: "upstream_signal", Severity: "info", Title: "Electron v40", Evidence: []string{"electron/electron"}, Suggestion: "Check"},
	}

	result := classifyFindings(findings)

	if result["findings"] != 4 {
		t.Errorf("findings count = %v, want 4", result["findings"])
	}

	clusters, ok := result["clusters"].([]map[string]any)
	if !ok || len(clusters) != 1 {
		t.Fatalf("clusters length = %d, want 1", len(clusters))
	}
	if clusters[0]["title"] != "Audio cluster" {
		t.Errorf("clusters[0].title = %v", clusters[0]["title"])
	}

	drift, ok := result["drift"].([]map[string]any)
	if !ok || len(drift) != 2 {
		t.Fatalf("drift length = %d, want 2 (drift + staleness)", len(drift))
	}

	upstream, ok := result["upstream"].([]map[string]any)
	if !ok || len(upstream) != 1 {
		t.Fatalf("upstream length = %d, want 1", len(upstream))
	}
}
```

- [ ] **Step 9: Run all tests**

Run: `go test ./internal/synthesis/ -v`
Expected: PASS

- [ ] **Step 10: Enrich /report/trends response in cmd/server/main.go**

Modify the `/report/trends` handler to include synthesis findings. Change the response struct:

```go
	mux.HandleFunc("/report/trends", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		weeks := parseWeeksParam(r.URL.Query().Get("weeks"))
		trends, trendsErr := s.GetWeeklyTrends(r.Context(), repo, weeks)
		if trends == nil {
			http.Error(w, "failed to get trends", http.StatusInternalServerError)
			return
		}

		// Fetch recent synthesis findings (last 30 days)
		since := time.Now().Add(-30 * 24 * time.Hour)
		findings, findingsErr := s.GetRecentFindings(r.Context(), repo, since)

		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			*store.WeeklyTrends
			Partial   bool                    `json:"partial"`
			Synthesis *store.SynthesisFindings `json:"synthesis,omitempty"`
		}{
			WeeklyTrends: trends,
			Partial:      trendsErr != nil,
			Synthesis:    findings,
		}
		if trendsErr != nil {
			logger.Warn("partial weekly trends", "error", trendsErr, "repo", repo)
		}
		if findingsErr != nil {
			logger.Warn("partial synthesis findings", "error", findingsErr, "repo", repo)
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Error("encoding trends response", "error", err)
		}
	})
```

Note: add `"time"` to the imports in `cmd/server/main.go` if not already present.

- [ ] **Step 11: Run full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 12: Run linter**

Run: `golangci-lint run ./...`
Expected: no errors

- [ ] **Step 13: Commit**

```bash
git add internal/store/report.go internal/store/report_test.go internal/synthesis/runner.go internal/synthesis/runner_test.go cmd/server/main.go
git commit -m "feat: enrich /report/trends with structured synthesis findings

Adds SynthesisFindings type grouping recent clusters, drift, and upstream
findings. The synthesis runner now stores classified findings in event
metadata. The /report/trends endpoint includes them in its response."
```

---

## Task 2: MCP Protocol Implementation

**Files:**
- Create: `internal/mcp/protocol.go`
- Create: `internal/mcp/protocol_test.go`

This implements the JSON-RPC 2.0 core for MCP: request/response types, message parsing, tool registration, and the stdio read loop. This is the reusable foundation — tools are registered separately.

- [ ] **Step 1: Write failing tests for JSON-RPC message parsing**

```go
// internal/mcp/protocol_test.go
package mcp

import (
	"encoding/json"
	"testing"
)

func TestParseRequest(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_health_status","arguments":{"repo":"owner/repo"}}}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "tools/call" {
		t.Errorf("Method = %q, want %q", req.Method, "tools/call")
	}
	if req.ID == nil {
		t.Fatal("ID should not be nil")
	}
}

func TestParseNotification(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.ID != nil {
		t.Error("notification should have nil ID")
	}
	if req.Method != "notifications/initialized" {
		t.Errorf("Method = %q", req.Method)
	}
}

func TestToolRegistration(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	s.RegisterTool(ToolDef{
		Name:        "echo",
		Description: "echoes input",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}, func(args json.RawMessage) (any, error) {
		return map[string]string{"echo": "hello"}, nil
	})

	tools := s.ListTools()
	if len(tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("tool name = %q, want %q", tools[0].Name, "echo")
	}
}

func TestToolCall(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	s.RegisterTool(ToolDef{
		Name:        "greet",
		Description: "returns greeting",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(args json.RawMessage) (any, error) {
		return map[string]string{"message": "hello"}, nil
	})

	result, err := s.CallTool("greet", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]string)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if m["message"] != "hello" {
		t.Errorf("message = %q", m["message"])
	}
}

func TestCallUnknownTool(t *testing.T) {
	s := NewServer("test-server", "1.0.0")
	_, err := s.CallTool("nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestResponseJSON(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  json.RawMessage(`{"ok":true}`),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement MCP protocol**

```go
// internal/mcp/protocol.go
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

type registeredTool struct {
	def     ToolDef
	handler ToolHandler
}

// Server is a minimal MCP server that handles JSON-RPC 2.0 over stdio.
type Server struct {
	name    string
	version string
	tools   map[string]registeredTool
	logger  *slog.Logger
}

// NewServer creates a new MCP server.
func NewServer(name, version string) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   make(map[string]registeredTool),
		logger:  slog.Default(),
	}
}

// RegisterTool adds a tool to the server.
func (s *Server) RegisterTool(def ToolDef, handler ToolHandler) {
	s.tools[def.Name] = registeredTool{def: def, handler: handler}
}

// ListTools returns all registered tool definitions.
func (s *Server) ListTools() []ToolDef {
	result := make([]ToolDef, 0, len(s.tools))
	for _, t := range s.tools {
		result = append(result, t.def)
	}
	return result
}

// CallTool invokes a tool by name with the given arguments.
func (s *Server) CallTool(name string, args json.RawMessage) (any, error) {
	tool, ok := s.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool.handler(args)
}

// Run starts the stdio read loop, processing JSON-RPC messages until EOF.
func (s *Server) Run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// MCP messages can be large (e.g. synthesis findings).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.logger.Warn("invalid JSON-RPC message", "error", err)
			continue
		}

		// Notifications (no ID) don't get a response.
		if req.ID == nil {
			s.handleNotification(req)
			continue
		}

		resp := s.handleRequest(req)
		data, err := json.Marshal(resp)
		if err != nil {
			s.logger.Error("marshal response", "error", err)
			continue
		}
		data = append(data, '\n')
		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	return scanner.Err()
}

func (s *Server) handleNotification(req Request) {
	// notifications/initialized — no action needed.
	s.logger.Debug("notification received", "method", req.Method)
}

func (s *Server) handleRequest(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func (s *Server) handleInitialize(req Request) Response {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
	}
	data, _ := json.Marshal(result)
	return Response{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsList(req Request) Response {
	tools := s.ListTools()
	result := map[string]any{"tools": tools}
	data, _ := json.Marshal(result)
	return Response{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsCall(req Request) Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32602, Message: "invalid params: " + err.Error()},
		}
	}

	result, err := s.CallTool(params.Name, params.Arguments)
	if err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32603, Message: err.Error()},
		}
	}

	// MCP tools return content as an array of content blocks.
	resultJSON, _ := json.Marshal(result)
	content := []map[string]any{
		{"type": "text", "text": string(resultJSON)},
	}
	responseData, _ := json.Marshal(map[string]any{"content": content})
	return Response{JSONRPC: "2.0", ID: req.ID, Result: responseData}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -v`
Expected: PASS

- [ ] **Step 5: Run linter**

Run: `golangci-lint run ./internal/mcp/...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/protocol.go internal/mcp/protocol_test.go
git commit -m "feat: add MCP protocol implementation (JSON-RPC 2.0 over stdio)

Reusable MCP server with tool registration, JSON-RPC request/response
handling, and a stdio read loop. Foundation for the triage bot MCP tools."
```

---

## Task 3: MCP Tools — get_health_status and get_report_trends

**Files:**
- Create: `internal/mcp/tools/health_status.go`
- Create: `internal/mcp/tools/health_status_test.go`
- Create: `internal/mcp/tools/report_trends.go`
- Create: `internal/mcp/tools/report_trends_test.go`

These two tools call the triage bot's HTTP endpoints and return the data. They use `net/http` to call the existing endpoints rather than importing internal store packages — this keeps the MCP binary decoupled from the database. The MCP server connects to the triage bot via its base URL (Cloud Run in production, localhost in dev).

- [ ] **Step 1: Write failing test for health_status tool**

```go
// internal/mcp/tools/health_status_test.go
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthStatusTool(t *testing.T) {
	// Mock the triage bot /health-check endpoint (POST)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health-check" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s, want POST", r.Method)
		}
		if r.URL.Query().Get("repo") != "owner/repo" {
			t.Errorf("unexpected repo param: %s", r.URL.Query().Get("repo"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"metrics": map[string]any{
				"confidence_recent_7d": 85.5,
				"stuck_session_count":  0,
			},
			"alerts": []any{},
		})
	}))
	defer server.Close()

	tool := NewHealthStatusTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty result")
	}
}

func TestHealthStatusToolDef(t *testing.T) {
	tool := NewHealthStatusTool("http://localhost", "")
	if tool.Def.Name != "get_health_status" {
		t.Errorf("name = %q", tool.Def.Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/tools/ -run TestHealthStatus -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement health_status tool**

```go
// internal/mcp/tools/health_status.go
package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// Tool wraps a tool definition and its handler together.
type Tool struct {
	Def     mcp.ToolDef
	Handler mcp.ToolHandler
}

// NewHealthStatusTool creates the get_health_status MCP tool.
// baseURL is the triage bot HTTP base URL. secret is the INGEST_SECRET for auth.
func NewHealthStatusTool(baseURL, secret string) Tool {
	return Tool{
		Def: mcp.ToolDef{
			Name:        "get_health_status",
			Description: "Returns operational health metrics: confidence score trends, stuck sessions, orphaned triage. Use to check if the triage bot is healthy.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"repo": {
						"type": "string",
						"description": "Repository in owner/name format"
					}
				},
				"required": ["repo"]
			}`),
		},
		Handler: func(args json.RawMessage) (any, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			// /health-check is POST, not GET.
			return postJSON(baseURL+"/health-check?repo="+params.Repo, secret)
		},
	}
}

// fetchJSON makes a GET request and returns the parsed JSON response.
func fetchJSON(url, secret string) (any, error) {
	return doRequest(http.MethodGet, url, secret)
}

// postJSON makes a POST request and returns the parsed JSON response.
func postJSON(url, secret string) (any, error) {
	return doRequest(http.MethodPost, url, secret)
}

// doRequest makes an HTTP request and returns the parsed JSON response.
func doRequest(method, url, secret string) (any, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/tools/ -run TestHealthStatus -v`
Expected: PASS

- [ ] **Step 5: Write failing test for report_trends tool**

```go
// internal/mcp/tools/report_trends_test.go
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportTrendsTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report/trends" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"repo":  "owner/repo",
			"weeks": 12,
			"synthesis": map[string]any{
				"clusters": []any{},
				"drift":    []any{},
				"upstream": []any{},
			},
		})
	}))
	defer server.Close()

	tool := NewReportTrendsTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","weeks":12}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if len(data) == 0 {
		t.Error("empty result")
	}
}

func TestReportTrendsToolDef(t *testing.T) {
	tool := NewReportTrendsTool("http://localhost", "")
	if tool.Def.Name != "get_report_trends" {
		t.Errorf("name = %q", tool.Def.Name)
	}
}
```

- [ ] **Step 6: Implement report_trends tool**

```go
// internal/mcp/tools/report_trends.go
package tools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewReportTrendsTool creates the get_report_trends MCP tool.
func NewReportTrendsTool(baseURL, secret string) Tool {
	return Tool{
		Def: mcp.ToolDef{
			Name:        "get_report_trends",
			Description: "Returns weekly trend data (triage volume, phase hits, response times, agent outcomes, synthesis findings) and recent synthesis findings (clusters, drift, upstream impacts).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"repo": {
						"type": "string",
						"description": "Repository in owner/name format"
					},
					"weeks": {
						"type": "integer",
						"description": "Number of weeks of trend data (default 12, max 52)",
						"default": 12
					}
				},
				"required": ["repo"]
			}`),
		},
		Handler: func(args json.RawMessage) (any, error) {
			var params struct {
				Repo  string `json:"repo"`
				Weeks int    `json:"weeks"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if params.Weeks <= 0 {
				params.Weeks = 12
			}
			url := baseURL + "/report/trends?repo=" + params.Repo + "&weeks=" + strconv.Itoa(params.Weeks)
			return fetchJSON(url, secret)
		},
	}
}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/mcp/tools/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/tools/
git commit -m "feat: add get_health_status and get_report_trends MCP tools

HTTP-backed tools that call the triage bot's existing endpoints. Includes
shared fetchJSON helper and httptest-based unit tests."
```

---

## Task 4: MCP Tools — get_pending_triage and get_synthesis_briefing

**Files:**
- Create: `internal/mcp/tools/pending_triage.go`
- Create: `internal/mcp/tools/pending_triage_test.go`
- Create: `internal/mcp/tools/synthesis_briefing.go`
- Create: `internal/mcp/tools/synthesis_briefing_test.go`

These tools call the existing `/report` (dashboard stats) and `/report/trends` endpoints respectively, extracting the relevant subset of data.

- [ ] **Step 1: Write failing test for pending_triage tool**

```go
// internal/mcp/tools/pending_triage_test.go
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPendingTriageTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"triage_stats": map[string]any{
				"total":    10,
				"promoted": 3,
				"pending":  5,
				"recent": []any{
					map[string]any{
						"repo":         "owner/repo",
						"issue_number": 42,
						"shadow_repo":  "owner/shadow",
						"shadow_issue": 7,
						"promoted":     false,
						"created_at":   "2026-03-28T10:00:00Z",
					},
				},
			},
			"agent_stats": map[string]any{
				"total":            5,
				"stage_breakdown":  map[string]any{},
				"action_breakdown": map[string]any{},
				"recent":           []any{},
			},
		})
	}))
	defer server.Close()

	tool := NewPendingTriageTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if len(data) == 0 {
		t.Error("empty result")
	}
}

func TestPendingTriageToolDef(t *testing.T) {
	tool := NewPendingTriageTool("http://localhost", "")
	if tool.Def.Name != "get_pending_triage" {
		t.Errorf("name = %q", tool.Def.Name)
	}
}
```

- [ ] **Step 2: Implement pending_triage tool**

```go
// internal/mcp/tools/pending_triage.go
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewPendingTriageTool creates the get_pending_triage MCP tool.
func NewPendingTriageTool(baseURL, secret string) Tool {
	return Tool{
		Def: mcp.ToolDef{
			Name:        "get_pending_triage",
			Description: "Returns pending triage and research shadow issues awaiting review (lgtm/reject), plus agent session status. Use to see what needs maintainer attention.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"repo": {
						"type": "string",
						"description": "Repository in owner/name format"
					}
				},
				"required": ["repo"]
			}`),
		},
		Handler: func(args json.RawMessage) (any, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			return fetchJSON(baseURL+"/report?repo="+params.Repo, secret)
		},
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/mcp/tools/ -run TestPendingTriage -v`
Expected: PASS

- [ ] **Step 4: Write failing test for synthesis_briefing tool**

```go
// internal/mcp/tools/synthesis_briefing_test.go
package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSynthesisBriefingTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report/trends" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"repo":  "owner/repo",
			"weeks": 4,
			"synthesis": map[string]any{
				"clusters": []any{
					map[string]any{"title": "Audio cluster", "severity": "warning"},
				},
				"drift":    []any{},
				"upstream": []any{},
			},
		})
	}))
	defer server.Close()

	tool := NewSynthesisBriefingTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if len(data) == 0 {
		t.Error("empty result")
	}
}

func TestSynthesisBriefingToolDef(t *testing.T) {
	tool := NewSynthesisBriefingTool("http://localhost", "")
	if tool.Def.Name != "get_synthesis_briefing" {
		t.Errorf("name = %q", tool.Def.Name)
	}
}
```

- [ ] **Step 5: Implement synthesis_briefing tool**

```go
// internal/mcp/tools/synthesis_briefing.go
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewSynthesisBriefingTool creates the get_synthesis_briefing MCP tool.
// Returns recent synthesis findings (last 4 weeks) from the enriched /report/trends endpoint.
func NewSynthesisBriefingTool(baseURL, secret string) Tool {
	return Tool{
		Def: mcp.ToolDef{
			Name:        "get_synthesis_briefing",
			Description: "Returns recent synthesis findings: issue clusters, ADR drift signals, and upstream dependency impacts from the last 4 weekly briefings. Use to understand what patterns the synthesis engine has detected.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"repo": {
						"type": "string",
						"description": "Repository in owner/name format"
					}
				},
				"required": ["repo"]
			}`),
		},
		Handler: func(args json.RawMessage) (any, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			// Use 4 weeks to get approximately the last 4 briefings.
			return fetchJSON(baseURL+"/report/trends?repo="+params.Repo+"&weeks=4", secret)
		},
	}
}
```

- [ ] **Step 6: Run all tool tests**

Run: `go test ./internal/mcp/tools/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/tools/pending_triage.go internal/mcp/tools/pending_triage_test.go internal/mcp/tools/synthesis_briefing.go internal/mcp/tools/synthesis_briefing_test.go
git commit -m "feat: add get_pending_triage and get_synthesis_briefing MCP tools

Pending triage wraps /report for shadow issue queue. Synthesis briefing
wraps the enriched /report/trends for recent cluster/drift/upstream findings."
```

---

## Task 5: MCP Server Entry Point

**Files:**
- Create: `cmd/mcp/main.go`

Wires all four tools and starts the stdio server. Configured via environment variables: `TRIAGE_BOT_URL` (base URL for HTTP calls, defaults to `http://localhost:8080`) and `INGEST_SECRET` (optional auth for Cloud Run).

- [ ] **Step 1: Implement cmd/mcp/main.go**

```go
// cmd/mcp/main.go
package main

import (
	"log/slog"
	"os"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp/tools"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	baseURL := os.Getenv("TRIAGE_BOT_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	secret := os.Getenv("INGEST_SECRET")

	server := mcp.NewServer("triage-bot", "1.0.0")

	healthTool := tools.NewHealthStatusTool(baseURL, secret)
	trendsTool := tools.NewReportTrendsTool(baseURL, secret)
	pendingTool := tools.NewPendingTriageTool(baseURL, secret)
	briefingTool := tools.NewSynthesisBriefingTool(baseURL, secret)

	server.RegisterTool(healthTool.Def, healthTool.Handler)
	server.RegisterTool(trendsTool.Def, trendsTool.Handler)
	server.RegisterTool(pendingTool.Def, pendingTool.Handler)
	server.RegisterTool(briefingTool.Def, briefingTool.Handler)

	logger.Info("triage-bot MCP server starting", "base_url", baseURL, "tools", 4)

	if err := server.Run(os.Stdin, os.Stdout); err != nil {
		logger.Error("MCP server error", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build the binary**

Run: `go build -o mcp-server ./cmd/mcp`
Expected: builds without errors.

- [ ] **Step 3: Run full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: all PASS

- [ ] **Step 4: Run linter**

Run: `golangci-lint run ./...`
Expected: no errors

- [ ] **Step 5: Clean up build artifact**

Run: `rm mcp-server`

- [ ] **Step 6: Commit**

```bash
git add cmd/mcp/main.go
git commit -m "feat: add triage-bot MCP server entry point

Wires four tools (get_health_status, get_report_trends, get_pending_triage,
get_synthesis_briefing) and starts a stdio JSON-RPC server. Configured via
TRIAGE_BOT_URL and INGEST_SECRET env vars."
```

---

## Task 6: Update CLAUDE.md and Documentation

**Files:**
- Modify: `CLAUDE.md` — add MCP server to project structure and commands

- [ ] **Step 1: Update CLAUDE.md project structure**

Add `cmd/mcp/main.go` to the project structure section:

```
cmd/mcp/main.go               # MCP server (stdio JSON-RPC, wraps HTTP endpoints for Claude Code agents)
```

Add to the internal packages section:

```
internal/mcp/protocol.go      # MCP JSON-RPC 2.0 protocol implementation
internal/mcp/tools/            # MCP tool implementations (health, trends, triage, briefing)
```

- [ ] **Step 2: Add MCP commands to Essential Commands**

Add after the "Run locally" section:

```bash
# Run MCP server (requires running triage bot at TRIAGE_BOT_URL)
TRIAGE_BOT_URL=https://triage-bot-lhuutxzbnq-uc.a.run.app go run ./cmd/mcp

# Add to Claude Code
claude mcp add triage-bot -- go run ./cmd/mcp
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add MCP server to CLAUDE.md project structure and commands"
```

---

## Task 7: End-to-End Verification

This task verifies the complete chain works without requiring a running database.

- [ ] **Step 1: Run all tests**

Run: `go test ./... 2>&1 | tail -30`
Expected: all PASS, including new tests in `internal/mcp/`, `internal/mcp/tools/`, `internal/synthesis/`, `internal/store/`

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: no issues

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./...`
Expected: no errors

- [ ] **Step 4: Verify all three binaries build**

Run: `go build ./cmd/server && go build ./cmd/mcp && go build ./cmd/seed && rm -f server mcp-server seed`
Expected: all build without errors

- [ ] **Step 5: Verify MCP server starts and responds to initialize**

Run:
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | go run ./cmd/mcp 2>/dev/null
```
Expected: JSON response containing `"protocolVersion"` and `"serverInfo"`.

- [ ] **Step 6: Verify tools/list returns all four tools**

Run:
```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n{"jsonrpc":"2.0","method":"notifications/initialized"}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | go run ./cmd/mcp 2>/dev/null | tail -1
```
Expected: JSON response containing `get_health_status`, `get_report_trends`, `get_pending_triage`, `get_synthesis_briefing`.
