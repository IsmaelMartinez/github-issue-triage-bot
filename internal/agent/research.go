package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
)

type EnhancementAnalysis struct {
	NeedsClarification bool              `json:"needs_clarification"`
	Questions          []ClarifyQuestion `json:"questions"`
	Confidence         float64           `json:"confidence"`
}

type ClarifyQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

type ResearchDocument struct {
	Title          string     `json:"title"`
	Summary        string     `json:"summary"`
	Approaches     []Approach `json:"approaches"`
	Recommendation string     `json:"recommendation"`
	OpenQuestions  []string   `json:"open_questions"`
}

type Approach struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Pros        []string `json:"pros"`
	Cons        []string `json:"cons"`
}

const analyzeEnhancementSystemPrompt = `You are a software engineering analyst. Analyze the given enhancement request and determine if it has enough detail to proceed with research.

A well-specified enhancement should describe:
- The desired behavior or outcome
- The problem it solves or the motivation
- Any known constraints or requirements

If the enhancement lacks clarity, generate up to 3 clarifying questions. Prefer multiple-choice questions where possible.

Respond with JSON matching this schema:
{
  "needs_clarification": boolean,
  "questions": [{"question": "string", "options": ["string"]}],
  "confidence": number between 0 and 1
}

Set confidence to how confident you are that you understand the request (1.0 = perfectly clear, 0.0 = completely unclear).
If needs_clarification is false, questions should be an empty array.`

const synthesizeResearchSystemPrompt = `You are a software engineering researcher. Given an enhancement request and optional related context, produce a research document that evaluates implementation approaches.

Generate 2-3 distinct approaches with trade-offs. Be specific and actionable.

Respond with JSON matching this schema:
{
  "title": "string",
  "summary": "string",
  "approaches": [{"name": "string", "description": "string", "pros": ["string"], "cons": ["string"]}],
  "recommendation": "string",
  "open_questions": ["string"]
}`

// AnalyzeEnhancement uses an LLM to determine if an enhancement request has
// enough detail to proceed with research, returning clarifying questions if not.
func AnalyzeEnhancement(ctx context.Context, provider llm.Provider, title, body string) (*EnhancementAnalysis, error) {
	userContent := fmt.Sprintf("Enhancement title: %s\n\nEnhancement body:\n%s", title, body)

	raw, err := provider.GenerateJSONWithSystem(ctx, analyzeEnhancementSystemPrompt, userContent, 0.3, 2048)
	if err != nil {
		return nil, fmt.Errorf("analyze enhancement: %w", err)
	}

	var result EnhancementAnalysis
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse enhancement analysis: %w", err)
	}

	return &result, nil
}

// SynthesizeResearch uses an LLM to produce a research document with multiple
// implementation approaches, trade-offs, and a recommendation.
func SynthesizeResearch(ctx context.Context, provider llm.Provider, title, body string, relatedDocs []string, relatedIssues []string) (*ResearchDocument, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Enhancement title: %s\n\nEnhancement body:\n%s", title, body)

	if len(relatedDocs) > 0 {
		sb.WriteString("\n\n--- Related Documentation ---\n")
		sb.WriteString(joinWithIndex(relatedDocs))
	}

	if len(relatedIssues) > 0 {
		sb.WriteString("\n\n--- Related Issues ---\n")
		sb.WriteString(joinWithIndex(relatedIssues))
	}

	raw, err := provider.GenerateJSONWithSystem(ctx, synthesizeResearchSystemPrompt, sb.String(), 0.5, 8192)
	if err != nil {
		return nil, fmt.Errorf("synthesize research: %w", err)
	}

	var result ResearchDocument
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse research document: %w", err)
	}

	return &result, nil
}

// FormatResearchMarkdown converts a ResearchDocument into a markdown string
// suitable for posting as a GitHub issue or comment.
func FormatResearchMarkdown(doc *ResearchDocument, sourceRepo string, issueNumber int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s\n\n", doc.Title)
	fmt.Fprintf(&sb, "> Research document for %s#%d\n\n", sourceRepo, issueNumber)

	sb.WriteString("## Summary\n\n")
	sb.WriteString(doc.Summary)
	sb.WriteString("\n\n")

	sb.WriteString("## Approaches\n\n")
	for i, approach := range doc.Approaches {
		fmt.Fprintf(&sb, "### %d. %s\n\n", i+1, approach.Name)
		sb.WriteString(approach.Description)
		sb.WriteString("\n\n")

		sb.WriteString("**Pros:**\n")
		for _, pro := range approach.Pros {
			fmt.Fprintf(&sb, "- %s\n", pro)
		}

		sb.WriteString("\n**Cons:**\n")
		for _, con := range approach.Cons {
			fmt.Fprintf(&sb, "- %s\n", con)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Recommendation\n\n")
	sb.WriteString(doc.Recommendation)
	sb.WriteString("\n\n")

	sb.WriteString("## Open Questions\n\n")
	for _, q := range doc.OpenQuestions {
		fmt.Fprintf(&sb, "- %s\n", q)
	}

	return sb.String()
}

// joinWithIndex formats a slice of strings as indexed lines: "[0] item\n[1] item\n..."
func joinWithIndex(items []string) string {
	var sb strings.Builder
	for i, item := range items {
		fmt.Fprintf(&sb, "[%d] %s\n", i, item)
	}
	return sb.String()
}
