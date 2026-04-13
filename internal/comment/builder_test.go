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
		IsBug:        true,
		DocsURL:      "https://example.com/docs",
		DebugCommand: "my-app --debug",
		Phase1: phases.Phase1Result{
			MissingItems: []phases.MissingItem{
				{Label: "Reproduction steps", Detail: "Step-by-step instructions"},
				{Label: "Debug console output", Detail: "Log output"},
			},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "checked this issue") {
		t.Error("missing preamble")
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
	if !strings.Contains(got, "my-app --debug") {
		t.Error("debug instructions should contain configured debug command")
	}
	if !strings.Contains(got, "Project docs") {
		t.Error("missing project docs link in footer")
	}
	if !strings.Contains(got, "Bot suggestion") {
		t.Error("missing bot disclosure")
	}
	if !strings.Contains(got, "share feedback") {
		t.Error("missing feedback link")
	}
}

func TestBuild_BugWithMissingInfoNoDebugCommand(t *testing.T) {
	result := TriageResult{
		IsBug: true,
		Phase1: phases.Phase1Result{
			MissingItems: []phases.MissingItem{
				{Label: "Debug console output", Detail: "Log output"},
				{Label: "Reproduction steps", Detail: "Step-by-step instructions"},
			},
		},
	}
	got := Build(result)

	if strings.Contains(got, "How to get debug logs") {
		t.Error("should omit debug instructions when DebugCommand is empty")
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

	if !strings.Contains(got, "checked this issue") {
		t.Error("missing preamble")
	}
	preambleIdx := strings.Index(got, "checked this issue")
	pwaIdx := strings.Index(got, "Teams web app")
	if preambleIdx >= pwaIdx {
		t.Error("preamble should appear before the PWA note")
	}
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
		DocsURL:       "https://example.com/docs",
		Phase4a: []phases.ContextMatch{
			{Topic: "Dark Mode", Status: "planned", DocURL: "https://example.com/dark", Source: "roadmap", Reason: "appears related"},
		},
	}
	got := Build(result)

	if !strings.Contains(got, "checked this issue") {
		t.Error("missing preamble")
	}
	if !strings.Contains(got, "Related work") {
		t.Error("missing context header")
	}
	if !strings.Contains(got, "[Dark Mode](https://example.com/dark)") {
		t.Error("missing context link")
	}
	if !strings.Contains(got, "Project docs") {
		t.Error("missing project docs link in footer")
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

func TestBuild_FeedbackLink(t *testing.T) {
	feedbackURL := "https://github.com/IsmaelMartinez/github-issue-triage-bot/issues/new?template=bot-feedback.yml"

	bugResult := TriageResult{
		IsBug: true,
		Phase1: phases.Phase1Result{
			MissingItems: []phases.MissingItem{
				{Label: "Reproduction steps", Detail: "Step-by-step instructions"},
			},
		},
	}
	bugGot := Build(bugResult)
	if !strings.Contains(bugGot, feedbackURL) {
		t.Error("bug comment missing feedback template URL")
	}
	if !strings.Contains(bugGot, "[share feedback]") {
		t.Error("bug comment missing 'share feedback' link text")
	}

	enhancementResult := TriageResult{
		IsEnhancement: true,
		Phase4a: []phases.ContextMatch{
			{Topic: "Dark Mode", Status: "planned", DocURL: "https://example.com/dark", Source: "roadmap", Reason: "related"},
		},
	}
	enhGot := Build(enhancementResult)
	if !strings.Contains(enhGot, feedbackURL) {
		t.Error("enhancement comment missing feedback template URL")
	}
	if !strings.Contains(enhGot, "[share feedback]") {
		t.Error("enhancement comment missing 'share feedback' link text")
	}
}

func TestBuild_PreambleAppearsFirst(t *testing.T) {
	result := TriageResult{
		IsBug: true,
		Phase2: []phases.Suggestion{
			{Title: "Known crash", DocURL: "https://example.com/crash", Reason: "similar crash report"},
		},
	}
	got := Build(result)

	preamble := "I checked this issue against the project's documentation and known issues."
	if !strings.HasPrefix(got, preamble) {
		t.Errorf("output should start with preamble, got: %q", got[:min(len(got), 100)])
	}
}

func TestBuild_UnlabelledIssueWithPhase2(t *testing.T) {
	result := TriageResult{
		IsBug:         false,
		IsEnhancement: false,
		DocsURL:       "https://example.com/docs",
		Phase2: []phases.Suggestion{
			{Title: "Network Timeout", DocURL: "https://example.com/timeout", Reason: "similar network timeout reported"},
		},
	}
	got := Build(result)

	if got == "" {
		t.Fatal("Build() should not return empty when Phase2 has suggestions for unlabelled issue")
	}
	if !strings.Contains(got, "checked this issue") {
		t.Error("missing preamble")
	}
	if !strings.Contains(got, "Possibly related") {
		t.Error("missing suggestions header")
	}
	if !strings.Contains(got, "[Network Timeout](https://example.com/timeout)") {
		t.Error("missing suggestion link")
	}
	if !strings.Contains(got, "Project docs") {
		t.Error("missing project docs link in footer")
	}
	if !strings.Contains(got, "share feedback") {
		t.Error("missing feedback link")
	}
}
