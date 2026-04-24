package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestBuildContextBrief(t *testing.T) {
	mock := &mockProvider{
		response: `{"summary": "This enhancement requests adding keyboard shortcuts for common call actions. It matters because users currently must use mouse clicks for frequent operations."}`,
	}

	docs := []store.SimilarDocument{
		{Document: store.Document{DocType: "adr", Title: "ADR-001: Shortcut Framework", Content: "We decided to use Electron accelerators for keyboard shortcuts."}, Distance: 0.1},
		{Document: store.Document{DocType: "roadmap", Title: "Q3 Roadmap", Content: "Keyboard accessibility improvements planned for Q3."}, Distance: 0.2},
		{Document: store.Document{DocType: "research", Title: "Accessibility Research", Content: "Research into screen reader and keyboard navigation patterns."}, Distance: 0.3},
		{Document: store.Document{DocType: "adr", Title: "ADR-002: Input Handling", Content: "Input events are captured at the webview level."}, Distance: 0.15},
	}

	issues := []store.SimilarIssue{
		{Issue: store.Issue{Number: 42, Title: "Add mute shortcut", State: "closed", Summary: "User requested Ctrl+M for mute toggle."}, Distance: 0.1},
		{Issue: store.Issue{Number: 99, Title: "Keyboard nav broken", State: "open", Summary: "Tab navigation does not work in chat."}, Distance: 0.2},
	}

	brief, err := BuildContextBrief(context.Background(), mock, "Add keyboard shortcuts", "I want shortcuts for mute and camera toggle", docs, issues, "owner/repo", 55)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if brief.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(brief.ADRs) != 2 {
		t.Errorf("expected 2 ADRs, got %d", len(brief.ADRs))
	}
	if len(brief.Roadmap) != 1 {
		t.Errorf("expected 1 roadmap doc, got %d", len(brief.Roadmap))
	}
	if len(brief.Research) != 1 {
		t.Errorf("expected 1 research doc, got %d", len(brief.Research))
	}
	if len(brief.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(brief.Issues))
	}

	md := FormatContextBriefMarkdown(brief)

	checks := []struct {
		label    string
		contains string
	}{
		{"title header", "## Context Brief: Add keyboard shortcuts"},
		{"issue ref", "owner/repo#55"},
		{"summary text", "keyboard shortcuts"},
		{"adr section", "### Architecture Decisions"},
		{"adr title", "ADR-001: Shortcut Framework"},
		{"roadmap section", "### Roadmap"},
		{"research section", "### Prior Research"},
		{"issues section", "### Related Issues"},
		{"issue number", "#42"},
		{"issue state", "(closed)"},
		{"issue summary", "Ctrl+M for mute toggle"},
		{"footer", "Reply `research`"},
	}

	for _, check := range checks {
		if !strings.Contains(md, check.contains) {
			t.Errorf("%s: expected markdown to contain %q", check.label, check.contains)
		}
	}
}

func TestBuildContextBrief_EmptyResults(t *testing.T) {
	mock := &mockProvider{
		response: `{"summary": "This enhancement requests a new notification system for incoming messages."}`,
	}

	brief, err := BuildContextBrief(context.Background(), mock, "Add notifications", "I want desktop notifications", nil, nil, "owner/repo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if brief.Summary == "" {
		t.Error("expected non-empty summary")
	}

	md := FormatContextBriefMarkdown(brief)

	if !strings.Contains(md, "## Context Brief: Add notifications") {
		t.Error("expected title header in markdown")
	}
	if !strings.Contains(md, "notification system") {
		t.Error("expected summary text in markdown")
	}

	absent := []string{"### Architecture Decisions", "### Roadmap", "### Prior Research", "### Related Issues"}
	for _, section := range absent {
		if strings.Contains(md, section) {
			t.Errorf("expected markdown NOT to contain %q with empty results", section)
		}
	}

	if !strings.Contains(md, "Reply `research`") {
		t.Error("expected footer in markdown")
	}
}

func TestFormatContextBriefMarkdown_NeutralizesMentions(t *testing.T) {
	brief := &ContextBrief{
		Summary:    "Summary referencing @summaryUser.",
		SourceRepo: "owner/repo",
		IssueNum:   7,
		Title:      "Test",
		Issues: []store.SimilarIssue{
			{Issue: store.Issue{Number: 1938, Title: "Extended MQTT status", State: "open", Summary: "great work of @Donnyp751 allows to propagate status"}},
		},
	}

	md := FormatContextBriefMarkdown(brief)

	if strings.Contains(md, "@Donnyp751") {
		t.Errorf("expected @Donnyp751 to be neutralized, got:\n%s", md)
	}
	if !strings.Contains(md, "Donnyp751") {
		t.Errorf("expected username to be preserved without the @, got:\n%s", md)
	}
	if strings.Contains(md, "@summaryUser") {
		t.Errorf("expected @summaryUser to be neutralized, got:\n%s", md)
	}
}

func TestNeutralizeMentions(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"great work of @Donnyp751", "great work of Donnyp751"},
		{"@someone at start", "someone at start"},
		{"hello @a, @b and @c.", "hello a, b and c."},
		{"email@domain.com stays", "email@domain.com stays"},
		{"trailing punctuation @user!", "trailing punctuation user!"},
		{"org team ref @owner/team keeps slash", "org team ref owner/team keeps slash"},
		{"double at @@user collapses", "double at user collapses"},
		{"no mentions here", "no mentions here"},
		{"", ""},
	}
	for _, c := range cases {
		got := neutralizeMentions(c.in)
		if got != c.want {
			t.Errorf("neutralizeMentions(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
