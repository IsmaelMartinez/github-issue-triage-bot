package store

import "testing"

func TestDailyBucketStructure(t *testing.T) {
	b := DailyBucket{Date: "2026-03-17", Count: 5}
	if b.Date != "2026-03-17" || b.Count != 5 {
		t.Fatalf("unexpected DailyBucket: %+v", b)
	}
}

func TestClampWeeks(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 12},
		{-5, 12},
		{8, 8},
		{52, 52},
		{100, 52},
		{1, 1},
	}
	for _, tt := range tests {
		got := ClampWeeks(tt.input)
		if got != tt.want {
			t.Errorf("ClampWeeks(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestWeeklyTriageType(t *testing.T) {
	wt := WeeklyTriage{Week: "2026-03-17", Total: 10, Promoted: 7, Rate: 0.7}
	if wt.Week != "2026-03-17" || wt.Total != 10 || wt.Promoted != 7 || wt.Rate != 0.7 {
		t.Fatalf("unexpected WeeklyTriage: %+v", wt)
	}
}

func TestWeeklyAgentsType(t *testing.T) {
	wa := WeeklyAgents{Week: "2026-03-10", Total: 5, Approved: 2, Rejected: 1, Pending: 1, Complete: 1}
	if wa.Week != "2026-03-10" || wa.Total != 5 || wa.Approved != 2 || wa.Rejected != 1 || wa.Pending != 1 || wa.Complete != 1 {
		t.Fatalf("unexpected WeeklyAgents: %+v", wa)
	}
}

func TestSynthesisFindingsModel(t *testing.T) {
	f := SynthesisFindings{
		AsOf: "2026-03-31T00:00:00Z",
		Clusters: []FindingSummary{
			{Title: "Auth cluster", Severity: "warning", Suggestion: "investigate"},
		},
		Drift: []FindingSummary{
			{Title: "ADR-003 stale", Severity: "action_needed", Suggestion: "update ADR"},
		},
		Upstream: []FindingSummary{
			{Title: "Electron v34 impact", Severity: "info"},
		},
	}
	if len(f.Clusters) != 1 || f.Clusters[0].Title != "Auth cluster" {
		t.Fatalf("unexpected Clusters: %+v", f.Clusters)
	}
	if len(f.Drift) != 1 || f.Drift[0].Severity != "action_needed" {
		t.Fatalf("unexpected Drift: %+v", f.Drift)
	}
	if len(f.Upstream) != 1 || f.Upstream[0].Title != "Electron v34 impact" {
		t.Fatalf("unexpected Upstream: %+v", f.Upstream)
	}
	if f.AsOf != "2026-03-31T00:00:00Z" {
		t.Fatalf("unexpected AsOf: %s", f.AsOf)
	}
}
