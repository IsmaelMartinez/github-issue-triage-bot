package comment

import (
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
)

func TestBuild_EmptyResult(t *testing.T) {
	result := TriageResult{IsBug: true}
	got := Build(result)
	if got != "" {
		t.Errorf("Build() should return empty for no findings, got %q", got)
	}
}

func TestBuild_BugWithMissingInfo(t *testing.T) {
	result := TriageResult{
		IsBug: true,
		Phase1: phases.Phase1Result{
			MissingItems: []phases.MissingItem{
				{Label: "Reproduction steps", Detail: "Step-by-step instructions"},
				{Label: "Debug console output", Detail: "Log output"},
			},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "Reproduction steps") {
		t.Error("missing reproduction steps item")
	}
	if !strings.Contains(got, "Debug console output") {
		t.Error("missing debug output item")
	}
	if !strings.Contains(got, "How to get debug logs") {
		t.Error("missing debug instructions")
	}
	if !strings.Contains(got, "Troubleshooting Guide") {
		t.Error("missing troubleshooting guide link")
	}
	if !strings.Contains(got, "Bot suggestion") {
		t.Error("missing bot disclosure")
	}
	if !strings.Contains(got, "@ismael-triage-bot") {
		t.Error("missing feedback mention hint")
	}
}

func TestBuild_BugWithPWA(t *testing.T) {
	result := TriageResult{
		IsBug: true,
		Phase1: phases.Phase1Result{
			IsPwaReproducible: true,
		},
	}
	got := Build(result)

	if !strings.Contains(got, "Teams web app") {
		t.Error("missing PWA note")
	}
	if !strings.Contains(got, "Microsoft Feedback Portal") {
		t.Error("missing feedback portal link")
	}
}

func TestBuild_BugWithSuggestions(t *testing.T) {
	result := TriageResult{
		IsBug: true,
		Phase1: phases.Phase1Result{IsPwaReproducible: true},
		Phase2: []phases.Suggestion{
			{Title: "Cache Issue", DocURL: "https://example.com/cache", Reason: "appears similar to a login caching issue. Try clearing cache."},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "Possibly related") {
		t.Error("missing suggestions header")
	}
	if !strings.Contains(got, "[Cache Issue](https://example.com/cache)") {
		t.Error("missing suggestion link")
	}
}

func TestBuild_Phase4aSanitizesNonInfeasibleBranch(t *testing.T) {
	result := TriageResult{
		IsEnhancement: true,
		Phase4a: []phases.ContextMatch{
			{Topic: "[evil](javascript:alert(1))", Status: "planned", DocURL: "javascript:alert(1)", Source: "roadmap", Reason: "related"},
		},
	}
	got := Build(result)

	if strings.Contains(got, "javascript:") {
		t.Error("Phase 4a non-infeasible branch was not sanitized: javascript: scheme still present")
	}
}

func TestBuild_Enhancement(t *testing.T) {
	result := TriageResult{
		IsEnhancement: true,
		Phase4a: []phases.ContextMatch{
			{Topic: "Dark Mode", Status: "planned", DocURL: "https://example.com/dark", Source: "roadmap", Reason: "appears related"},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "Related work") {
		t.Error("missing context header")
	}
	if !strings.Contains(got, "[Dark Mode](https://example.com/dark)") {
		t.Error("missing context link")
	}
	if !strings.Contains(got, "Roadmap") {
		t.Error("missing roadmap tip link")
	}
}

func TestBuild_DocBugSkipsPwaAndDebug(t *testing.T) {
	r := TriageResult{
		IsBug:    true,
		IsDocBug: true,
		Phase1: phases.Phase1Result{
			IsPwaReproducible: true,
			MissingItems: []phases.MissingItem{
				{Label: "Debug console output", Detail: "Log output"},
				{Label: "Reproduction steps", Detail: "Steps to trigger"},
			},
		},
	}
	got := Build(r)
	if strings.Contains(got, "web app") {
		t.Error("doc bug should not include PWA note")
	}
	if strings.Contains(got, "Debug console output") {
		t.Error("doc bug should not ask for debug logs")
	}
	if strings.Contains(got, "ELECTRON_ENABLE_LOGGING") {
		t.Error("doc bug should not include debug instructions")
	}
	if !strings.Contains(got, "Reproduction steps") {
		t.Error("doc bug should still include reproduction steps")
	}
}

func TestBuild_DocBugAllFilteredReturnsEmpty(t *testing.T) {
	r := TriageResult{
		IsBug:    true,
		IsDocBug: true,
		Phase1: phases.Phase1Result{
			MissingItems: []phases.MissingItem{
				{Label: "Debug console output", Detail: "Log output"},
			},
		},
	}
	got := Build(r)
	if got != "" {
		t.Errorf("doc bug with only filtered items should return empty, got: %q", got)
	}
}
