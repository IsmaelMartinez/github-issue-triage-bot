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

	if !strings.Contains(got, "Thanks for reporting this issue") {
		t.Error("missing bug greeting")
	}
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
	if !strings.Contains(got, "I'm a bot") {
		t.Error("missing bot disclosure")
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

	if !strings.Contains(got, "Microsoft Teams web app") {
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

	if !strings.Contains(got, "known issue") {
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
	lastUpdated := "2026-01-15"
	result := TriageResult{
		IsEnhancement: true,
		Phase4a: []phases.ContextMatch{
			{Topic: "Dark Mode", Status: "planned", DocURL: "https://example.com/dark", Source: "roadmap", LastUpdated: &lastUpdated, Reason: "appears related"},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "Thanks for the feature suggestion") {
		t.Error("missing enhancement greeting")
	}
	if !strings.Contains(got, "previously explored") {
		t.Error("missing context header")
	}
	if !strings.Contains(got, "[Dark Mode](https://example.com/dark)") {
		t.Error("missing context link")
	}
	if !strings.Contains(got, "Development Roadmap") {
		t.Error("missing roadmap tip link")
	}
}

