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
