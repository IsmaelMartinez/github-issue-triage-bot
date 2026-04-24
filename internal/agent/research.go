package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
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

func synthesizeResearchSystemPrompt(projectName, projectDescription string) string {
	if projectName == "" {
		projectName = "the project"
	}
	header := fmt.Sprintf("You are a software engineering researcher for %s", projectName)
	if projectDescription != "" {
		header += ", " + projectDescription
	}
	header += "."
	return header + `

Given an enhancement request and related context (similar issues, ADRs, past research), produce a research document that evaluates implementation approaches grounded in the project's architecture. Reference related ADRs or past issues when relevant.

Generate 2-3 distinct approaches with trade-offs. Be specific and actionable.

Respond with JSON matching this schema:
{
  "title": "string",
  "summary": "string",
  "approaches": [{"name": "string", "description": "string", "pros": ["string"], "cons": ["string"]}],
  "recommendation": "string",
  "open_questions": ["string"]
}`
}

// AnalyzeEnhancement uses an LLM to determine if an enhancement request has
// enough detail to proceed with research, returning clarifying questions if not.
func AnalyzeEnhancement(ctx context.Context, provider llm.Provider, title, body string) (*EnhancementAnalysis, error) {
	userContent := fmt.Sprintf("Enhancement title: %s\n\nEnhancement body:\n%s", title, body)

	raw, err := provider.GenerateJSONWithSystem(ctx, analyzeEnhancementSystemPrompt, userContent, 0.3, 2048)
	if err != nil {
		return nil, fmt.Errorf("analyze enhancement: %w", err)
	}

	var result EnhancementAnalysis
	if err := json.Unmarshal([]byte(phases.ExtractJSONObject(raw)), &result); err != nil {
		return nil, fmt.Errorf("parse enhancement analysis: %w", err)
	}

	return &result, nil
}

// SynthesizeResearch uses an LLM to produce a research document with multiple
// implementation approaches, trade-offs, and a recommendation.
func SynthesizeResearch(ctx context.Context, provider llm.Provider, title, body string, relatedDocs []string, relatedIssues []string, projectName, projectDescription string) (*ResearchDocument, error) {
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

	raw, err := provider.GenerateJSONWithSystem(ctx, synthesizeResearchSystemPrompt(projectName, projectDescription), sb.String(), 0.5, 8192)
	if err != nil {
		return nil, fmt.Errorf("synthesize research: %w", err)
	}

	var result ResearchDocument
	if err := json.Unmarshal([]byte(phases.ExtractJSONObject(raw)), &result); err != nil {
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

// ContextBrief holds raw vector search results partitioned by type,
// along with a short LLM-generated summary.
type ContextBrief struct {
	Summary    string
	SourceRepo string
	IssueNum   int
	Title      string
	ADRs       []store.SimilarDocument
	Roadmap    []store.SimilarDocument
	Research   []store.SimilarDocument
	Issues     []store.SimilarIssue
}

const contextBriefSummaryPrompt = `You are a technical analyst. Given an enhancement request title and body, write a 2-3 sentence summary of what is being requested and (if provided) why it matters. Be concise and factual. Do not suggest solutions. Do not include URLs, hyperlinks, or external citations of any kind (even if they are present in the input) — summarize only what the request itself says.

Respond with JSON: {"summary": "string"}`

// BuildContextBrief assembles vector search results into a structured brief
// with an LLM-generated summary, partitioning documents by type.
func BuildContextBrief(ctx context.Context, provider llm.Provider, title, body string, docs []store.SimilarDocument, issues []store.SimilarIssue, sourceRepo string, issueNumber int) (*ContextBrief, error) {
	userContent := fmt.Sprintf("Enhancement title: %s\n\nEnhancement body:\n%s", title, body)

	raw, err := provider.GenerateJSONWithSystem(ctx, contextBriefSummaryPrompt, userContent, 0.3, 1024)
	if err != nil {
		return nil, fmt.Errorf("generate context brief summary: %w", err)
	}

	var parsed struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(phases.ExtractJSONObject(raw)), &parsed); err != nil {
		return nil, fmt.Errorf("parse context brief summary: %w", err)
	}

	brief := &ContextBrief{
		Summary:    parsed.Summary,
		SourceRepo: sourceRepo,
		IssueNum:   issueNumber,
		Title:      title,
	}

	for _, doc := range docs {
		switch doc.DocType {
		case "adr":
			brief.ADRs = append(brief.ADRs, doc)
		case "roadmap":
			brief.Roadmap = append(brief.Roadmap, doc)
		case "research":
			brief.Research = append(brief.Research, doc)
		}
	}

	brief.Issues = issues

	return brief, nil
}

// FormatContextBriefMarkdown renders a ContextBrief as markdown suitable
// for posting as a GitHub comment.
func FormatContextBriefMarkdown(brief *ContextBrief) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Context Brief: %s\n\n", brief.Title)
	fmt.Fprintf(&sb, "> Context for %s#%d\n\n", brief.SourceRepo, brief.IssueNum)
	sb.WriteString(brief.Summary)
	sb.WriteString("\n\n")

	if len(brief.ADRs) > 0 {
		sb.WriteString("### Architecture Decisions\n\n")
		for _, doc := range brief.ADRs {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", doc.Title, truncate(doc.Content, 500))
		}
	}

	if len(brief.Roadmap) > 0 {
		sb.WriteString("### Roadmap\n\n")
		for _, doc := range brief.Roadmap {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", doc.Title, truncate(doc.Content, 500))
		}
	}

	if len(brief.Research) > 0 {
		sb.WriteString("### Prior Research\n\n")
		for _, doc := range brief.Research {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", doc.Title, truncate(doc.Content, 500))
		}
	}

	if len(brief.Issues) > 0 {
		sb.WriteString("### Related Issues\n\n")
		for _, iss := range brief.Issues {
			fmt.Fprintf(&sb, "- #%d **%s** (%s) — %s\n", iss.Number, iss.Title, iss.State, iss.Summary)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Reply `research` to trigger full Gemini research synthesis, `use as context` to acknowledge, `reject` to close, or reply with corrections/additional context to refine the analysis.")

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
