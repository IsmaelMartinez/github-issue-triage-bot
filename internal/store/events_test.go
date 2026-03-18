package store

import "testing"

func TestRepoEventModel(t *testing.T) {
	e := RepoEvent{
		Repo:      "owner/repo",
		EventType: "issue_opened",
		SourceRef: "#42",
		Summary:   "New bug report about audio",
		Areas:     []string{"audio", "electron"},
		Metadata:  map[string]any{"labels": []string{"bug"}},
	}

	if e.Repo != "owner/repo" {
		t.Errorf("Repo = %q", e.Repo)
	}
	if e.EventType != "issue_opened" {
		t.Errorf("EventType = %q", e.EventType)
	}
	if len(e.Areas) != 2 {
		t.Errorf("Areas length = %d, want 2", len(e.Areas))
	}
}
