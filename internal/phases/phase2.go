package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Pre-compiled regex for stripping code fences across phases.
var reStripCodeFences = regexp.MustCompile("(?s)```[\\s\\S]*?```")

// Phase2 searches for matching troubleshooting documentation using vector similarity
// and then asks the LLM to pick the best matches with actionable suggestions.
func Phase2(ctx context.Context, s store.PhaseQuerier, l llm.Provider, logger *slog.Logger, repo, title, body string, codeContext string, preEmbedding []float32, projectName string) ([]Suggestion, error) {
	logger.Info("phase2 start")
	cleanBody := stripCodeFences(body, 1500)
	queryText := fmt.Sprintf("%s\n%s", truncate(title, 200), cleanBody)

	// Use pre-computed embedding if provided, otherwise compute it
	var embedding []float32
	if len(preEmbedding) > 0 {
		embedding = preEmbedding
	} else {
		var err error
		embedding, err = l.Embed(ctx, queryText)
		if err != nil {
			return nil, fmt.Errorf("embed issue: %w", err)
		}
	}
	// Find similar documents across all doc types (project + upstream)
	docs, err := s.FindSimilarDocuments(ctx, repo, []string{"troubleshooting", "configuration", "adr", "roadmap", "research", "upstream_release", "upstream_issue"}, embedding, 5)
	if err != nil {
		return nil, fmt.Errorf("find similar docs: %w", err)
	}
	logger.Info("phase2 vector search", "documents", len(docs))
	if len(docs) == 0 {
		logger.Info("phase2 finish", "suggestions", 0)
		return nil, nil
	}

	// Build compact summaries for the LLM
	var summaries []string
	for i, d := range docs {
		meta := d.Metadata
		category, _ := meta["category"].(string)
		desc, _ := meta["description"].(string)
		summaries = append(summaries, fmt.Sprintf("[%d] %s (%s): %s", i, d.Title, category, truncate(desc, 200)))
	}

	if projectName == "" {
		projectName = "this"
	}
	systemPrompt := fmt.Sprintf(`You are a helpful assistant for the %q open source project.
Match this issue against our documentation: troubleshooting guides, configuration options, architecture decisions (ADRs), roadmap items, and research documents.`, projectName) + `

Return a JSON array of 0-3 matches. Only include sections with a strong connection (same symptoms, same error message, same component, or a documented decision/limitation that explains the behaviour). For each match, estimate a relevance percentage (60-95). Only include matches above 60%%.

Keep the reason concise (1-2 sentences combining the explanation and what the user should try or be aware of).

Format: [{"index": 0, "reason": "This might be related because both involve login failures with SSO providers. Try clearing the cache and restarting.", "relevance": 75}]

If no sections match, return: []
Respond with ONLY valid JSON, no other text.`
	if codeContext != "" {
		systemPrompt += "\n\nYou also have access to relevant source code from the repository. Use it to:\n- Identify specific configuration options or code paths related to the bug\n- Suggest specific debug log lines or config values the user should check\n- Provide more targeted diagnostic steps based on the actual implementation"
	}
	userContent := fmt.Sprintf("KNOWN ISSUES:\n%s\n\nISSUE:\nTitle: %s\nBody: %s",
		strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)
	if codeContext != "" {
		userContent += "\n\n" + codeContext
	}

	raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.3, 8192)
	if err != nil {
		return nil, fmt.Errorf("generate suggestions: %w", err)
	}
	// Parse and validate response
	raw = extractJSONArray(raw)
	var matches []struct {
		Index     int    `json:"index"`
		Reason    string `json:"reason"`
		Relevance int    `json:"relevance"`
	}
	if err := json.Unmarshal([]byte(raw), &matches); err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	// Per-category relevance thresholds: troubleshooting needs high precision
	// (wrong suggestions waste user time), other doc types are lower risk
	// (informational context rather than actionable troubleshooting steps).
	categoryThresholds := map[string]int{
		"troubleshooting":  70,
		"configuration":    50,
		"adr":              55,
		"roadmap":          55,
		"research":         55,
		"upstream_release": 50,
		"upstream_issue":   45,
	}
	defaultThreshold := 60

	var results []Suggestion
	for _, m := range matches {
		if m.Index < 0 || m.Index >= len(docs) || m.Reason == "" || m.Relevance > 100 {
			continue
		}
		threshold := defaultThreshold
		if t, ok := categoryThresholds[docs[m.Index].DocType]; ok {
			threshold = t
		}
		if m.Relevance < threshold {
			continue
		}
		docURL, _ := docs[m.Index].Metadata["docUrl"].(string)
		results = append(results, Suggestion{
			Title:  docs[m.Index].Title,
			DocURL: docURL,
			Reason: truncate(m.Reason, 400),
		})
		if len(results) >= 3 {
			break
		}
	}
	logger.Info("phase2 finish", "suggestions", len(results))
	return results, nil
}

// Helper functions shared across phases

func stripCodeFences(text string, maxLen int) string {
	result := reStripCodeFences.ReplaceAllString(text, "")
	return truncate(result, maxLen)
}

// StripCodeFences removes markdown code fences and truncates to maxLen.
func StripCodeFences(text string, maxLen int) string {
	return stripCodeFences(text, maxLen)
}

// truncate shortens s to at most maxLen bytes, backing up to a valid UTF-8
// rune boundary so multi-byte sequences are never split.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk back from maxLen until we land on the start of a UTF-8 rune.
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}

// Truncate shortens s to at most maxLen bytes at a valid UTF-8 boundary.
func Truncate(s string, maxLen int) string {
	return truncate(s, maxLen)
}

// extractJSONArray finds the first top-level JSON array in raw by matching
// balanced brackets, avoiding the greedy-regex problem of matching the first
// '[' to the last ']'.
func extractJSONArray(raw string) string {
	return extractBalanced(raw, '[', ']', "[]")
}

// ExtractJSONObject finds the first top-level JSON object in raw by matching
// balanced braces.
func ExtractJSONObject(raw string) string {
	return extractBalanced(raw, '{', '}', "{}")
}

// extractBalanced finds the first occurrence of open in raw, then walks forward
// counting balanced open/close characters (skipping string literals) to find
// the matching close. Returns fallback if no balanced match is found.
func extractBalanced(raw string, open, close byte, fallback string) string {
	start := strings.IndexByte(raw, open)
	if start < 0 {
		return fallback
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return fallback
}
