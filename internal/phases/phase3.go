package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Phase3 detects potential duplicate issues using vector similarity search
// and LLM-based semantic comparison.
func Phase3(ctx context.Context, s *store.Store, l *llm.Client, repo string, issueNumber int, title, body string) ([]Duplicate, error) {
	cleanBody := stripCodeFences(body, 1500)
	queryText := fmt.Sprintf("%s\n%s", truncate(title, 200), cleanBody)

	// Get embedding for the issue
	embedding, err := l.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embed issue: %w", err)
	}

	// Find similar issues, excluding the current one
	issues, err := s.FindSimilarIssues(ctx, repo, embedding, issueNumber, 10)
	if err != nil {
		return nil, fmt.Errorf("find similar issues: %w", err)
	}
	if len(issues) == 0 {
		return nil, nil
	}

	// Build compact issue summaries for the LLM
	var summaries []string
	for _, iss := range issues {
		summaries = append(summaries, fmt.Sprintf("[#%d] (%s) %s: %s", iss.Number, iss.State, iss.Title, truncate(iss.Summary, 120)))
	}

	systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Compare this new issue against existing issues to find potential duplicates or closely related reports.

Return a JSON array of 0-3 matches. Only include issues with a strong semantic connection (same bug, same feature request, clearly overlapping symptoms). Use humble language in the reason field ("might be related", "appears similar", "could be the same issue").

For each match, estimate a similarity percentage (60-95). Only include matches above 60%%.

Format: [{"number": 123, "reason": "This might be related because...", "similarity": 75}]

If no issues are similar, return: []
Respond with ONLY valid JSON, no other text.`
	userContent := fmt.Sprintf("EXISTING ISSUES:\n%s\n\nNEW ISSUE:\nTitle: %s\nBody: %s",
		strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

	raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.2, 8192)
	if err != nil {
		return nil, fmt.Errorf("generate duplicates: %w", err)
	}
	// Parse and validate response
	raw = extractJSONArray(raw)
	var matches []struct {
		Number     int    `json:"number"`
		Reason     string `json:"reason"`
		Similarity int    `json:"similarity"`
	}
	if err := json.Unmarshal([]byte(raw), &matches); err != nil {
		return nil, fmt.Errorf("parse duplicates: %w", err)
	}

	// Build a lookup map from the similarity search results
	issueMap := make(map[int]store.SimilarIssue)
	for _, iss := range issues {
		issueMap[iss.Number] = iss
	}

	var results []Duplicate
	for _, m := range matches {
		iss, ok := issueMap[m.Number]
		if !ok || m.Reason == "" || m.Similarity < 60 || m.Similarity > 100 {
			continue
		}

		dup := Duplicate{
			Number:     m.Number,
			Title:      iss.Title,
			State:      iss.State,
			Reason:     truncate(m.Reason, 200),
			Similarity: m.Similarity,
			Milestone:  iss.Milestone,
		}
		if iss.ClosedAt != nil {
			formatted := iss.ClosedAt.Format("2006-01-02")
			dup.ClosedAt = &formatted
		}
		results = append(results, dup)
		if len(results) >= 3 {
			break
		}
	}
	return results, nil
}
