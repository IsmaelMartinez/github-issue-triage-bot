package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBriefPreviewToolDef(t *testing.T) {
	tool := NewBriefPreviewTool("http://localhost", "")
	if tool.Def.Name != "get_brief_preview" {
		t.Errorf("expected name get_brief_preview, got %q", tool.Def.Name)
	}
	if tool.Def.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestBriefPreviewToolMissingArgs(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"missing repo", `{"issue_number":1}`},
		{"empty repo", `{"repo":"","issue_number":1}`},
		{"missing issue_number", `{"repo":"owner/repo"}`},
		{"zero issue_number", `{"repo":"owner/repo","issue_number":0}`},
	}
	tool := NewBriefPreviewTool("http://localhost", "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Handler(json.RawMessage(tc.args))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestBriefPreviewToolHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/brief-preview" {
			t.Errorf("expected path /brief-preview, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if got["repo"] != "owner/repo" {
			t.Errorf("expected repo=owner/repo, got %v", got["repo"])
		}
		if got["issue_number"].(float64) != 42 {
			t.Errorf("expected issue_number=42, got %v", got["issue_number"])
		}
		if got["hat"] != "display-session-media" {
			t.Errorf("expected hat=display-session-media, got %v", got["hat"])
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"class":               "display-session-media",
			"similar_past_issues": []any{},
			"docs":                []any{},
			"regression_prs":      nil,
			"upstream_candidates": []int{},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewBriefPreviewTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","issue_number":42,"hat":"display-session-media"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if m["class"] != "display-session-media" {
		t.Errorf("class = %v, want display-session-media", m["class"])
	}
}

func TestBriefPreviewToolHatOptional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if _, present := got["hat"]; present {
			t.Errorf("expected hat to be omitted when empty, got %v", got["hat"])
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"class": "other"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewBriefPreviewTool(server.URL, "")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","issue_number":42}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBriefPreviewToolAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret" {
			t.Errorf("wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"class": "other"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewBriefPreviewTool(server.URL, "my-secret")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","issue_number":42}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBriefPreviewToolHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "repo not in allow-list", http.StatusForbidden)
	}))
	defer server.Close()

	tool := NewBriefPreviewTool(server.URL, "")
	_, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","issue_number":42}`))
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestBriefPreviewToolBadArgs(t *testing.T) {
	tool := NewBriefPreviewTool("http://localhost", "")
	_, err := tool.Handler(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatal("expected error for bad JSON args")
	}
}
