package agent

import (
	"context"
	"strings"
	"testing"
)

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) GenerateJSON(_ context.Context, _ string, _ float64, _ int) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) GenerateJSONWithSystem(_ context.Context, _, _ string, _ float64, _ int) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

func TestAnalyzeEnhancement_NeedsClarification(t *testing.T) {
	mock := &mockProvider{
		response: `{
			"needs_clarification": true,
			"questions": [
				{"question": "What platform are you targeting?", "options": ["Linux", "macOS", "Windows"]},
				{"question": "What is the expected behavior?"}
			],
			"confidence": 0.4
		}`,
	}

	result, err := AnalyzeEnhancement(context.Background(), mock, "Add dark mode", "I want dark mode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.NeedsClarification {
		t.Error("expected NeedsClarification to be true")
	}
	if len(result.Questions) != 2 {
		t.Errorf("expected 2 questions, got %d", len(result.Questions))
	}
	if result.Questions[0].Question != "What platform are you targeting?" {
		t.Errorf("unexpected first question: %s", result.Questions[0].Question)
	}
	if len(result.Questions[0].Options) != 3 {
		t.Errorf("expected 3 options for first question, got %d", len(result.Questions[0].Options))
	}
	if result.Confidence != 0.4 {
		t.Errorf("expected confidence 0.4, got %f", result.Confidence)
	}
}

func TestAnalyzeEnhancement_NoClarificationNeeded(t *testing.T) {
	mock := &mockProvider{
		response: `{
			"needs_clarification": false,
			"questions": [],
			"confidence": 0.95
		}`,
	}

	result, err := AnalyzeEnhancement(context.Background(), mock, "Add keyboard shortcut for mute", "When in a call, pressing Ctrl+M should toggle mute. Currently there is no keyboard shortcut.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NeedsClarification {
		t.Error("expected NeedsClarification to be false")
	}
	if len(result.Questions) != 0 {
		t.Errorf("expected 0 questions, got %d", len(result.Questions))
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.Confidence)
	}
}

func TestSynthesizeResearch(t *testing.T) {
	mock := &mockProvider{
		response: `{
			"title": "Dark Mode Implementation Research",
			"summary": "Analysis of approaches to implement dark mode support.",
			"approaches": [
				{
					"name": "CSS Variables",
					"description": "Use CSS custom properties to define theme colors.",
					"pros": ["Simple to implement", "Good browser support"],
					"cons": ["Limited to CSS-based theming"]
				},
				{
					"name": "Theme Provider Component",
					"description": "Create a React context-based theme provider.",
					"pros": ["Full control over theming", "Works with CSS-in-JS"],
					"cons": ["More complex", "Requires refactoring"]
				}
			],
			"recommendation": "CSS Variables approach is recommended for its simplicity.",
			"open_questions": ["Should we support system preference detection?", "What about high contrast mode?"]
		}`,
	}

	result, err := SynthesizeResearch(
		context.Background(), mock,
		"Add dark mode", "I want dark mode support",
		[]string{"Theming guide from docs"},
		[]string{"Issue #42: Color scheme request"},
		"", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Title != "Dark Mode Implementation Research" {
		t.Errorf("unexpected title: %s", result.Title)
	}
	if len(result.Approaches) < 2 {
		t.Errorf("expected at least 2 approaches, got %d", len(result.Approaches))
	}
	if result.Recommendation == "" {
		t.Error("expected non-empty recommendation")
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(result.OpenQuestions) != 2 {
		t.Errorf("expected 2 open questions, got %d", len(result.OpenQuestions))
	}
}

func TestFormatResearchMarkdown(t *testing.T) {
	doc := &ResearchDocument{
		Title:   "Widget Refactor Research",
		Summary: "Evaluating options for refactoring the widget system.",
		Approaches: []Approach{
			{
				Name:        "Incremental Migration",
				Description: "Migrate widgets one at a time.",
				Pros:        []string{"Low risk", "Can be done in parallel"},
				Cons:        []string{"Takes longer"},
			},
			{
				Name:        "Big Bang Rewrite",
				Description: "Rewrite all widgets at once.",
				Pros:        []string{"Clean slate"},
				Cons:        []string{"High risk", "Blocks other work"},
			},
		},
		Recommendation: "Incremental migration is safer.",
		OpenQuestions:   []string{"How many widgets exist?"},
	}

	output := FormatResearchMarkdown(doc, "owner/repo", 123)

	checks := []struct {
		label    string
		contains string
	}{
		{"title", "# Widget Refactor Research"},
		{"issue reference", "owner/repo#123"},
		{"summary heading", "## Summary"},
		{"summary content", "Evaluating options"},
		{"approaches heading", "## Approaches"},
		{"approach 1", "### 1. Incremental Migration"},
		{"approach 2", "### 2. Big Bang Rewrite"},
		{"pros label", "**Pros:**"},
		{"cons label", "**Cons:**"},
		{"pro item", "- Low risk"},
		{"con item", "- High risk"},
		{"recommendation heading", "## Recommendation"},
		{"recommendation content", "Incremental migration is safer."},
		{"open questions heading", "## Open Questions"},
		{"open question item", "- How many widgets exist?"},
	}

	for _, check := range checks {
		if !strings.Contains(output, check.contains) {
			t.Errorf("%s: expected output to contain %q", check.label, check.contains)
		}
	}
}

func TestJoinWithIndex(t *testing.T) {
	items := []string{"alpha", "beta", "gamma"}
	result := joinWithIndex(items)
	expected := "[0] alpha\n[1] beta\n[2] gamma\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
