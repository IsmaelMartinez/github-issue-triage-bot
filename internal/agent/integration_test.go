package agent

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// sequentialMockProvider returns responses in order, cycling through them.
type sequentialMockProvider struct {
	responses []string
	callIndex int
}

func (m *sequentialMockProvider) GenerateJSON(_ context.Context, _ string, _ float64, _ int) (string, error) {
	resp := m.responses[m.callIndex%len(m.responses)]
	m.callIndex++
	return resp, nil
}

func (m *sequentialMockProvider) GenerateJSONWithSystem(_ context.Context, _, _ string, _ float64, _ int) (string, error) {
	resp := m.responses[m.callIndex%len(m.responses)]
	m.callIndex++
	return resp, nil
}

func (m *sequentialMockProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

func TestEnhancementResearchFlow_SkipClarification(t *testing.T) {
	// The mock returns two responses in sequence:
	// 1. AnalyzeEnhancement: no clarification needed
	// 2. SynthesizeResearch: a valid research document
	mock := &sequentialMockProvider{
		responses: []string{
			`{
				"needs_clarification": false,
				"questions": [],
				"confidence": 0.92
			}`,
			`{
				"title": "Keyboard Shortcut Implementation",
				"summary": "Analysis of approaches to add Ctrl+M mute toggle.",
				"approaches": [
					{
						"name": "Electron globalShortcut API",
						"description": "Register a global shortcut using Electron's globalShortcut module.",
						"pros": ["Native OS integration", "Works when app is not focused"],
						"cons": ["May conflict with other apps"]
					},
					{
						"name": "DOM keydown listener",
						"description": "Listen for keydown events on the renderer process.",
						"pros": ["Simple implementation", "No conflicts"],
						"cons": ["Only works when app is focused"]
					}
				],
				"recommendation": "Use Electron globalShortcut for best UX.",
				"open_questions": ["Should the shortcut be configurable?"]
			}`,
		},
	}

	ctx := context.Background()

	// Phase 1: Analyze — should skip clarification
	analysis, err := AnalyzeEnhancement(ctx, mock, "Add Ctrl+M mute toggle", "When in a call, pressing Ctrl+M should toggle mute.")
	if err != nil {
		t.Fatalf("AnalyzeEnhancement: %v", err)
	}
	if analysis.NeedsClarification {
		t.Fatal("expected no clarification needed for detailed request")
	}
	if analysis.Confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", analysis.Confidence)
	}

	// Phase 2: Synthesize research (since clarification was skipped)
	doc, err := SynthesizeResearch(ctx, mock, "Add Ctrl+M mute toggle", "When in a call, pressing Ctrl+M should toggle mute.", nil, nil, "", "")
	if err != nil {
		t.Fatalf("SynthesizeResearch: %v", err)
	}
	if doc.Title == "" {
		t.Error("expected non-empty research title")
	}
	if len(doc.Approaches) < 2 {
		t.Errorf("expected at least 2 approaches, got %d", len(doc.Approaches))
	}

	// Phase 3: Format as markdown and verify structure
	md := FormatResearchMarkdown(doc, "owner/repo", 42)

	if !strings.Contains(md, "owner/repo#42") {
		t.Error("markdown should contain issue reference owner/repo#42")
	}
	if !strings.Contains(md, "# Keyboard Shortcut Implementation") {
		t.Error("markdown should contain the research title")
	}
	if !strings.Contains(md, "## Summary") {
		t.Error("markdown should contain Summary heading")
	}
	if !strings.Contains(md, "## Approaches") {
		t.Error("markdown should contain Approaches heading")
	}
	if !strings.Contains(md, "## Recommendation") {
		t.Error("markdown should contain Recommendation heading")
	}
}

func TestApprovalSignalFlow(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    ApprovalSignal
	}{
		{"lgtm approves", "lgtm", SignalApproved},
		{"revise with feedback", "revise: please focus on the second approach", SignalRevise},
		{"promote to public", "publish this to the public issue", SignalPromote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseApprovalSignal(tt.comment)
			if got != tt.want {
				t.Errorf("ParseApprovalSignal(%q) = %d, want %d", tt.comment, got, tt.want)
			}
		})
	}
}

func TestContextBriefFlow(t *testing.T) {
	mock := &mockProvider{
		response: `{"summary": "This enhancement requests adding keyboard shortcuts for common call actions."}`,
	}

	docs := []store.SimilarDocument{
		{Document: store.Document{DocType: "adr", Title: "ADR-003: Keyboard Handling", Content: "Decided to use Electron globalShortcut API for app-level shortcuts."}, Distance: 0.1},
		{Document: store.Document{DocType: "roadmap", Title: "UX Improvements Q2", Content: "Planned keyboard shortcuts and accessibility improvements."}, Distance: 0.2},
		{Document: store.Document{DocType: "research", Title: "Shortcut Conflicts Study", Content: "Research into OS-level shortcut conflicts across Linux desktop environments."}, Distance: 0.3},
	}
	issues := []store.SimilarIssue{
		{Issue: store.Issue{Number: 15, Title: "Global mute shortcut", State: "open", Summary: "User wants Ctrl+M for mute"}, Distance: 0.1},
		{Issue: store.Issue{Number: 88, Title: "Keyboard nav in chat", State: "closed", Summary: "Added arrow key navigation in chat list"}, Distance: 0.3},
	}

	ctx := context.Background()
	brief, err := BuildContextBrief(ctx, mock, "Add Ctrl+M mute toggle", "When in a call, pressing Ctrl+M should toggle mute.", docs, issues, "owner/repo", 42)
	if err != nil {
		t.Fatalf("BuildContextBrief: %v", err)
	}

	if brief.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(brief.ADRs) != 1 {
		t.Errorf("expected 1 ADR, got %d", len(brief.ADRs))
	}
	if len(brief.Roadmap) != 1 {
		t.Errorf("expected 1 roadmap item, got %d", len(brief.Roadmap))
	}
	if len(brief.Research) != 1 {
		t.Errorf("expected 1 research doc, got %d", len(brief.Research))
	}
	if len(brief.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(brief.Issues))
	}

	md := FormatContextBriefMarkdown(brief)

	checks := []string{
		"## Context Brief:",
		"owner/repo#42",
		"### Architecture Decisions",
		"ADR-003: Keyboard Handling",
		"### Roadmap",
		"UX Improvements Q2",
		"### Prior Research",
		"Shortcut Conflicts Study",
		"### Related Issues",
		"#15",
		"#88",
		"`research`",
		"`use as context`",
		"`reject`",
	}

	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown should contain %q", want)
		}
	}
}

func TestContextBriefSignalRouting(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    ApprovalSignal
	}{
		{"research triggers full pipeline", "research", SignalResearch},
		{"use as context acknowledges", "use as context", SignalUseAsContext},
		{"reject closes session", "reject", SignalReject},
		{"random comment ignored", "what about performance?", SignalNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseApprovalSignal(tt.comment)
			if got != tt.want {
				t.Errorf("ParseApprovalSignal(%q) = %d, want %d", tt.comment, got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Add Dark Mode Support", "add-dark-mode-support"},
		{"Fix bug #123!", "fix-bug-123"},
		{"", ""},
		{"  leading and trailing spaces  ", "leading-and-trailing-spaces"},
		{"multiple---dashes", "multiple-dashes"},
		{"UPPERCASE", "uppercase"},
		{"special@chars&here", "special-chars-here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimeNowDate(t *testing.T) {
	date := timeNowDate()
	matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, date)
	if err != nil {
		t.Fatalf("regex error: %v", err)
	}
	if !matched {
		t.Errorf("timeNowDate() = %q, want YYYY-MM-DD format", date)
	}
}
