package synthesis

import (
	"strings"
	"testing"
)

func TestBuildBriefing(t *testing.T) {
	findings := []Finding{
		{Type: "cluster", Severity: "warning", Title: "Audio issues cluster", Evidence: []string{"#42", "#43"}, Suggestion: "Consider investigating"},
		{Type: "staleness", Severity: "info", Title: "Roadmap item R-3 inactive", Evidence: []string{"roadmap.md"}, Suggestion: "Review priority"},
	}

	md := BuildBriefing("2026-04-20", findings)

	if len(md) == 0 {
		t.Fatal("empty briefing")
	}
	if !strings.Contains(md, "[Briefing]") {
		t.Error("missing briefing title")
	}
	if !strings.Contains(md, "Audio issues cluster") {
		t.Error("missing cluster finding")
	}
	if !strings.Contains(md, "Roadmap item R-3") {
		t.Error("missing staleness finding")
	}
	if !strings.Contains(md, "Emerging Patterns") {
		t.Error("missing Emerging Patterns section header")
	}
	if !strings.Contains(md, "Decision Health") {
		t.Error("missing Decision Health section header")
	}
}

func TestBuildBriefingEmpty(t *testing.T) {
	md := BuildBriefing("2026-04-20", nil)
	if !strings.Contains(md, "Quiet week") {
		t.Error("empty briefing should mention quiet week")
	}
}

func TestBuildBriefingActionNeeded(t *testing.T) {
	findings := []Finding{
		{Type: "cluster", Severity: "action_needed", Title: "Critical pattern", Evidence: []string{"#1"}, Suggestion: "Investigate now"},
	}
	md := BuildBriefing("2026-04-20", findings)
	if !strings.Contains(md, "[ACTION NEEDED]") {
		t.Error("action_needed severity should include [ACTION NEEDED] tag")
	}
}

func TestBuildBriefingUpstream(t *testing.T) {
	findings := []Finding{
		{Type: "upstream_signal", Severity: "info", Title: "Electron v40 released", Evidence: []string{"electron/electron"}, Suggestion: "Check ADR-007"},
	}
	md := BuildBriefing("2026-04-20", findings)
	if !strings.Contains(md, "Upstream Signals") {
		t.Error("missing Upstream Signals section header")
	}
}

func TestBuildBriefingAllSections(t *testing.T) {
	findings := []Finding{
		{Type: "cluster", Severity: "warning", Title: "Cluster A"},
		{Type: "drift", Severity: "warning", Title: "Drift B"},
		{Type: "staleness", Severity: "info", Title: "Stale C"},
		{Type: "upstream_signal", Severity: "info", Title: "Upstream D"},
	}
	md := BuildBriefing("2026-04-20", findings)

	for _, section := range []string{"Emerging Patterns", "Decision Health", "Upstream Signals"} {
		if !strings.Contains(md, section) {
			t.Errorf("missing section: %s", section)
		}
	}
	if !strings.Contains(md, "Cluster A") || !strings.Contains(md, "Drift B") ||
		!strings.Contains(md, "Stale C") || !strings.Contains(md, "Upstream D") {
		t.Error("missing one or more finding titles")
	}
}
