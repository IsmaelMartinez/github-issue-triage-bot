package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSynthesisBriefingTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/report/trends" {
			t.Errorf("expected path /report/trends, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("repo") != "owner/repo" {
			t.Errorf("expected repo=owner/repo, got %q", r.URL.Query().Get("repo"))
		}
		if r.URL.Query().Get("weeks") != "4" {
			t.Errorf("expected weeks=4, got %q", r.URL.Query().Get("weeks"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"findings": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewSynthesisBriefingTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSynthesisBriefingToolDef(t *testing.T) {
	tool := NewSynthesisBriefingTool("http://localhost", "")
	if tool.Def.Name != "get_synthesis_briefing" {
		t.Errorf("expected name get_synthesis_briefing, got %q", tool.Def.Name)
	}
	if tool.Def.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestSynthesisBriefingToolMissingRepo(t *testing.T) {
	tool := NewSynthesisBriefingTool("http://localhost", "")
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestSynthesisBriefingToolAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret" {
			t.Errorf("wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"findings": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewSynthesisBriefingTool(server.URL, "my-secret")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
