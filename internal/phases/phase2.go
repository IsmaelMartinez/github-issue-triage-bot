package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Phase2 searches for matching troubleshooting documentation using vector similarity
// and then asks the LLM to pick the best matches with actionable suggestions.
func Phase2(ctx context.Context, s *store.Store, l *llm.Client, repo, title, body string) ([]Suggestion, error) {
	cleanBody := stripCodeFences(body, 1500)
	queryText := fmt.Sprintf("%s\n%s", truncate(title, 200), cleanBody)

	// Get embedding for the issue
	embedding, err := l.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embed issue: %w", err)
	}
	// Find similar troubleshooting documents
	docs, err := s.FindSimilarDocuments(ctx, repo, []string{"troubleshooting", "configuration"}, embedding, 5)
	if err != nil {
		return nil, fmt.Errorf("find similar docs: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	// Build compact summaries for the LLM
	var summaries []string
	for i, d := range docs {
		meta := d.Metadata
		category, _ := meta["category"].(string)
		desc, _ := meta["description"].(string)
		summaries = append(summaries, fmt.Sprintf("[%d] %s (%s): %s", i, d.Title, category, truncate(desc, 150)))
	}

	systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Match this bug report against known issues from our documentation.

Return a JSON array of 0-3 matches. Only include sections with a meaningful connection (shared symptoms, similar error, same component). Use humble language in the reason field ("appears similar", "might be related", "could be connected").

Format: [{"index": 0, "reason": "This appears similar because...", "actionable_step": "Try clearing the cache..."}]

If no sections match, return: []
Respond with ONLY valid JSON, no other text.`
	userContent := fmt.Sprintf("KNOWN ISSUES:\n%s\n\nBUG REPORT:\nTitle: %s\nBody: %s",
		strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

	raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.3, 8192)
	if err != nil {
		return nil, fmt.Errorf("generate suggestions: %w", err)
	}
	// Parse and validate response
	raw = extractJSONArray(raw)
	var matches []struct {
		Index          int    `json:"index"`
		Reason         string `json:"reason"`
		ActionableStep string `json:"actionable_step"`
	}
	if err := json.Unmarshal([]byte(raw), &matches); err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	var results []Suggestion
	for _, m := range matches {
		if m.Index < 0 || m.Index >= len(docs) || m.Reason == "" || m.ActionableStep == "" {
			continue
		}
		docURL, _ := docs[m.Index].Metadata["docUrl"].(string)
		results = append(results, Suggestion{
			Title:          docs[m.Index].Title,
			DocURL:         docURL,
			Reason:         truncate(m.Reason, 200),
			ActionableStep: truncate(m.ActionableStep, 200),
		})
		if len(results) >= 3 {
			break
		}
	}
	return results, nil
}

// Helper functions shared across phases

func stripCodeFences(text string, maxLen int) string {
	re := regexp.MustCompile("(?s)```[\\s\\S]*?```")
	result := re.ReplaceAllString(text, "")
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func extractJSONArray(raw string) string {
	re := regexp.MustCompile(`\[[\s\S]*\]`)
	match := re.FindString(raw)
	if match != "" {
		return match
	}
	return "[]"
}

func extractJSONObject(raw string) string {
	re := regexp.MustCompile(`\{[\s\S]*\}`)
	match := re.FindString(raw)
	if match != "" {
		return match
	}
	return "{}"
}
