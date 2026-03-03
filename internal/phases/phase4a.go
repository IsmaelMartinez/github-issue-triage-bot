package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Phase4a matches enhancement requests against the feature index (roadmap, ADRs, research)
// using vector similarity search and LLM-based semantic matching.
func Phase4a(ctx context.Context, s *store.Store, l *llm.Client, repo, title, body string) ([]ContextMatch, error) {
	cleanBody := stripCodeFences(body, 1500)
	queryText := fmt.Sprintf("%s\n%s", truncate(title, 200), cleanBody)

	// Get embedding for the enhancement request
	embedding, err := l.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embed issue: %w", err)
	}

	// Find similar features/ADRs/research
	docs, err := s.FindSimilarDocuments(ctx, repo, []string{"roadmap", "adr", "research"}, embedding, 5)
	if err != nil {
		return nil, fmt.Errorf("find similar features: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	// Build compact summaries for the LLM
	var summaries []string
	for i, d := range docs {
		status, _ := d.Metadata["status"].(string)
		summary, _ := d.Metadata["summary"].(string)
		summaries = append(summaries, fmt.Sprintf("[%d] (%s) %s: %s", i, status, d.Title, truncate(summary, 120)))
	}

	systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Match this enhancement request against our existing roadmap items, architecture decisions (ADRs), and research documents.

Return a JSON array of 0-3 matches. Only include items with a meaningful connection to the enhancement request (same feature area, overlapping goals, related technical decisions).

For each match, include:
- "index": the item index number
- "reason": a brief explanation using humble language ("appears related", "might be connected", "could be relevant")
- "is_infeasible": true ONLY if the matched item has status "rejected" and the rejection reason clearly applies to this request. false otherwise.

Format: [{"index": 0, "reason": "We've previously investigated this area...", "is_infeasible": false}]

If no items match, return: []
Respond with ONLY valid JSON, no other text.`
	userContent := fmt.Sprintf("EXISTING FEATURES/DECISIONS/RESEARCH:\n%s\n\nENHANCEMENT REQUEST:\nTitle: %s\nBody: %s",
		strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

	raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.3, 8192)
	if err != nil {
		return nil, fmt.Errorf("generate context: %w", err)
	}

	// Parse and validate response
	raw = extractJSONArray(raw)
	var matches []struct {
		Index        int    `json:"index"`
		Reason       string `json:"reason"`
		IsInfeasible bool   `json:"is_infeasible"`
	}
	if err := json.Unmarshal([]byte(raw), &matches); err != nil {
		return nil, fmt.Errorf("parse context: %w", err)
	}

	var results []ContextMatch
	for _, m := range matches {
		if m.Index < 0 || m.Index >= len(docs) || m.Reason == "" {
			continue
		}
		doc := docs[m.Index]
		status, _ := doc.Metadata["status"].(string)
		docURL, _ := doc.Metadata["doc_url"].(string)
		source := doc.DocType
		var lastUpdated *string
		if lu, ok := doc.Metadata["last_updated"].(string); ok {
			lastUpdated = &lu
		}

		results = append(results, ContextMatch{
			Topic:        doc.Title,
			Status:       status,
			DocURL:       docURL,
			Source:       source,
			LastUpdated:  lastUpdated,
			Reason:       truncate(m.Reason, 200),
			IsInfeasible: m.IsInfeasible && status == "rejected",
		})
		if len(results) >= 3 {
			break
		}
	}
	return results, nil
}
