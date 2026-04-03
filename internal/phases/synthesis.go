package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
)

// SynthesisInput holds the inputs from all triage phases for LLM synthesis.
type SynthesisInput struct {
	IssueTitle    string
	IssueBody     string
	IsBug         bool
	IsEnhancement bool
	IsDocBug      bool
	Phase1        Phase1Result
	Phase2        []Suggestion
	Phase4a       []ContextMatch
}

// ShouldSynthesize returns true when the phase outputs contain enough material
// for synthesis to add value beyond what the template-based builder produces.
func ShouldSynthesize(input SynthesisInput) bool {
	hasPwaNote := input.IsBug && !input.IsDocBug && input.Phase1.IsPwaReproducible
	hasPhase2 := len(input.Phase2) > 0
	hasPhase4a := len(input.Phase4a) > 0
	hasMissing := len(input.Phase1.MissingItems) > 0

	// Nothing to synthesize at all.
	if !hasMissing && !hasPhase2 && !hasPhase4a && !hasPwaNote {
		return false
	}

	// Only Phase 1 output with fewer than 2 missing items and no doc matches
	// is too simple for an LLM call — the template builder handles it fine.
	if !hasPhase2 && !hasPhase4a && !hasPwaNote && len(input.Phase1.MissingItems) < 2 {
		return false
	}

	return true
}

const synthesisSystemPrompt = `You are a triage assistant for the "Teams for Linux" open-source project. Write a single, concise GitHub comment responding to a new issue. Write like a knowledgeable maintainer — direct, helpful, no filler.

Rules:
- Keep the response under 1500 characters.
- Never invent documentation links — only reference URLs from the CONTEXT below.
- Never greet the user or sign off.
- If context documents relate to missing information, connect them (e.g., "this looks like X, and debug logs would help confirm").
- Use markdown sparingly: short paragraphs, a checklist only when 3+ items are missing.
- If nothing useful was found, return exactly: EMPTY

Return a JSON object with a single field "comment" containing your markdown response (or "EMPTY").`

// Synthesize calls the LLM to produce a single cohesive comment from all phase
// outputs. Returns empty string if the LLM determines there is nothing useful
// to say.
func Synthesize(ctx context.Context, l llm.Provider, input SynthesisInput) (string, error) {
	userContent := buildSynthesisPrompt(input)

	raw, err := l.GenerateJSONWithSystem(ctx, synthesisSystemPrompt, userContent, 0.3, 2048)
	if err != nil {
		return "", fmt.Errorf("synthesize: %w", err)
	}

	raw = ExtractJSONObject(raw)

	var result struct {
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", fmt.Errorf("parse synthesis response: %w", err)
	}

	comment := strings.TrimSpace(result.Comment)
	if comment == "" || comment == "EMPTY" {
		return "", nil
	}
	return comment, nil
}

// buildSynthesisPrompt assembles the user prompt from whichever phases produced
// output.
func buildSynthesisPrompt(input SynthesisInput) string {
	var b strings.Builder

	b.WriteString("ISSUE:\nTitle: ")
	b.WriteString(truncate(input.IssueTitle, 200))
	b.WriteString("\nBody: ")
	b.WriteString(stripCodeFences(input.IssueBody, 1500))
	b.WriteString("\n")

	// Context section from Phase 2 and Phase 4a doc matches.
	if len(input.Phase2) > 0 || len(input.Phase4a) > 0 {
		b.WriteString("\nCONTEXT (only use these URLs):\n")
		for _, s := range input.Phase2 {
			b.WriteString(fmt.Sprintf("- [%s](%s): %s\n", s.Title, s.DocURL, s.Reason))
		}
		for _, c := range input.Phase4a {
			b.WriteString(fmt.Sprintf("- [%s](%s): %s\n", c.Topic, c.DocURL, c.Reason))
		}
	}

	// Missing information from Phase 1.
	if len(input.Phase1.MissingItems) > 0 {
		b.WriteString("\nMISSING INFORMATION:\n")
		for _, item := range input.Phase1.MissingItems {
			b.WriteString(fmt.Sprintf("- %s: %s\n", item.Label, item.Detail))
		}
	}

	// PWA note for bugs that reproduce on the web app.
	if input.IsBug && !input.IsDocBug && input.Phase1.IsPwaReproducible {
		b.WriteString("\nPWA NOTE: This bug also occurs on the Teams web app, suggesting a Microsoft-side issue. The Microsoft Feedback Portal URL is https://feedbackportal.microsoft.com/.\n")
	}

	b.WriteString("\nWrite a single cohesive comment.")
	return b.String()
}
