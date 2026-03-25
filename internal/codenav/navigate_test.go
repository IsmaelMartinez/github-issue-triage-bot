package codenav

import (
	"testing"
)

func TestParseFileList(t *testing.T) {
	raw := `["app/browser/tools/tokenCache.js", "app/config/index.js"]`
	got := parseFileList(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(got))
	}
	if got[0] != "app/browser/tools/tokenCache.js" {
		t.Errorf("unexpected first path: %q", got[0])
	}
	if got[1] != "app/config/index.js" {
		t.Errorf("unexpected second path: %q", got[1])
	}
}

func TestParseFileList_InvalidJSON(t *testing.T) {
	got := parseFileList("not valid json {{")
	if got != nil {
		t.Errorf("expected nil for invalid JSON, got %v", got)
	}
}

func TestFormatForPrompt(t *testing.T) {
	cc := CodeContext{
		Files: []FileContent{
			{Path: "app/main.go", Content: "package main"},
			{Path: "app/config.go", Content: "package app"},
		},
	}
	out := cc.FormatForPrompt()
	if out == "" {
		t.Fatal("expected non-empty output for non-empty CodeContext")
	}
	for _, f := range cc.Files {
		if !containsStr(out, f.Path) {
			t.Errorf("output missing file path %q", f.Path)
		}
		if !containsStr(out, f.Content) {
			t.Errorf("output missing file content for %q", f.Path)
		}
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	cc := CodeContext{}
	if got := cc.FormatForPrompt(); got != "" {
		t.Errorf("expected empty string for empty CodeContext, got %q", got)
	}
}

// containsStr is a simple helper to avoid importing strings in tests.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
