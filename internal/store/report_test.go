package store

import "testing"

func TestDailyBucketStructure(t *testing.T) {
	b := DailyBucket{Date: "2026-03-17", Count: 5}
	if b.Date != "2026-03-17" || b.Count != 5 {
		t.Fatalf("unexpected DailyBucket: %+v", b)
	}
}
