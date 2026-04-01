package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPendingTriageTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/report" {
			t.Errorf("expected path /report, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("repo") != "owner/repo" {
			t.Errorf("expected repo=owner/repo, got %q", r.URL.Query().Get("repo"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"pending": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewPendingTriageTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestPendingTriageToolDef(t *testing.T) {
	tool := NewPendingTriageTool("http://localhost", "")
	if tool.Def.Name != "get_pending_triage" {
		t.Errorf("expected name get_pending_triage, got %q", tool.Def.Name)
	}
	if tool.Def.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestPendingTriageToolMissingRepo(t *testing.T) {
	tool := NewPendingTriageTool("http://localhost", "")
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestPendingTriageToolAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret" {
			t.Errorf("wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"pending": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewPendingTriageTool(server.URL, "my-secret")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
