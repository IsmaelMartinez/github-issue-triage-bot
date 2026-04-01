package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportTrendsTool(t *testing.T) {
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
		if r.URL.Query().Get("weeks") != "12" {
			t.Errorf("expected weeks=12, got %q", r.URL.Query().Get("weeks"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"weeks": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewReportTrendsTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestReportTrendsToolCustomWeeks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("weeks") != "6" {
			t.Errorf("expected weeks=6, got %q", r.URL.Query().Get("weeks"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"weeks": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	tool := NewReportTrendsTool(server.URL, "")
	result, err := tool.Handler(json.RawMessage(`{"repo":"owner/repo","weeks":6}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestReportTrendsToolDef(t *testing.T) {
	tool := NewReportTrendsTool("http://localhost", "")
	if tool.Def.Name != "get_report_trends" {
		t.Errorf("expected name get_report_trends, got %q", tool.Def.Name)
	}
	if tool.Def.Description == "" {
		t.Error("expected non-empty description")
	}
	if tool.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestReportTrendsToolMissingRepo(t *testing.T) {
	tool := NewReportTrendsTool("http://localhost", "")
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}
