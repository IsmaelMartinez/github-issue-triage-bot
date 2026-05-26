package store

import "testing"

func TestApplyHatBoost_BoostsKeywordMatches(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "Generic troubleshooting", Content: "various tips"}, Distance: 0.25},
		{Document: Document{Title: "Wayland screen-share", Content: "ozone flags"}, Distance: 0.30},
	}
	got := ApplyHatBoost(docs, []string{"ozone", "wayland"}, 0.10)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Title != "Wayland screen-share" {
		t.Errorf("got[0].Title = %q, want wayland first", got[0].Title)
	}
}

func TestApplyHatBoost_EmptyKeywordsReturnsOriginal(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "a"}, Distance: 0.1},
		{Document: Document{Title: "b"}, Distance: 0.2},
	}
	got := ApplyHatBoost(docs, nil, 0.05)
	if got[0].Title != "a" || got[1].Title != "b" {
		t.Errorf("order changed with nil keywords: %v", got)
	}
}

func TestApplyHatBoost_ClampsAtZero(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "iframe test", Content: ""}, Distance: 0.02},
	}
	got := ApplyHatBoost(docs, []string{"iframe"}, 0.10)
	if got[0].Distance != 0 {
		t.Errorf("distance = %v, want 0 (clamped)", got[0].Distance)
	}
}

func TestApplyVersionBoost_ExactMatchRanksFirst(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "Old release notes", Metadata: map[string]any{"electron_version": "40"}}, Distance: 0.21},
		{Document: Document{Title: "Current release notes", Metadata: map[string]any{"electron_version": "42"}}, Distance: 0.25},
		{Document: Document{Title: "Adjacent release notes", Metadata: map[string]any{"electron_version": "41"}}, Distance: 0.23},
	}
	got := ApplyVersionBoost(docs, "42", 0.05, 0.02)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Current (0.25 - 0.05 = 0.20) < Adjacent (0.23 - 0.02 = 0.21) = Old (0.21)
	if got[0].Title != "Current release notes" {
		t.Errorf("got[0].Title = %q, want exact-match version first", got[0].Title)
	}
	if got[2].Title != "Adjacent release notes" {
		t.Errorf("got[2].Title = %q, want adjacent version last (stable sort preserves Old before Adjacent)", got[2].Title)
	}
}

func TestApplyVersionBoost_EmptyVersionReturnsOriginal(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "a", Metadata: map[string]any{"electron_version": "42"}}, Distance: 0.1},
		{Document: Document{Title: "b", Metadata: map[string]any{"electron_version": "41"}}, Distance: 0.2},
	}
	got := ApplyVersionBoost(docs, "", 0.05, 0.02)
	if got[0].Title != "a" || got[1].Title != "b" {
		t.Errorf("order changed with empty version: %v", got)
	}
}

func TestApplyHatBoost_CaseInsensitive(t *testing.T) {
	docs := []SimilarDocument{
		{Document: Document{Title: "Wayland Session", Content: ""}, Distance: 0.5},
		{Document: Document{Title: "Other", Content: ""}, Distance: 0.3},
	}
	got := ApplyHatBoost(docs, []string{"WAYLAND"}, 0.3)
	if got[0].Title != "Wayland Session" {
		t.Errorf("case-insensitive boost failed: %v", got)
	}
}
