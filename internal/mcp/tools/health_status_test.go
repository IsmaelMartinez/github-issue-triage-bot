package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthStatusTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/health-check" {
			t.Errorf("expected path /health-check, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("repo") != "owner/repo" {
			t.Errorf("expected repo=owner/repo, got %q", r.URL.Query().Get("repo"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "healthy", "stuck_sessions": 0}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewHealthStatusTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestHealthStatusToolDef(t *testing.T) {
	tool := NewHealthStatusTool("http://localhost", "")
	if tool.Def.Name != "get_health_status" {
		t.Errorf("expected name get_health_status, got %q", tool.Def.Name)
	}
	if tool.Def.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestHealthStatusToolMissingRepo(t *testing.T) {
	tool := NewHealthStatusTool("http://localhost", "")
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestHealthStatusToolAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret" {
			t.Errorf("wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewHealthStatusTool(server.URL, "my-secret")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
