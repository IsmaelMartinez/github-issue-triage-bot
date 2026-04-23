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
