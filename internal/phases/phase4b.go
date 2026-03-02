package phases

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
)

// Phase4b detects misclassified issues using LLM classification.
// No vector search needed — just sends the issue to the LLM for classification.
func Phase4b(ctx context.Context, l *llm.Client, title, body, currentLabel string) (*Misclassification, error) {
	cleanBody := stripCodeFences(body, 1500)

	prompt := fmt.Sprintf(`You are a classification assistant for the "Teams for Linux" open source project.

Classify this GitHub issue as one of: bug, enhancement, or question.

ISSUE:
Title: %s
Body: %s

Current label: %s

Classification rules:
- "bug": Something that used to work and broke, a crash, an error, unexpected behavior, a regression
- "enhancement": A new feature request, an improvement to existing functionality, a UI change suggestion
- "question": A how-to question, a request for help or documentation

Return a JSON object with:
- "classification": one of "bug", "enhancement", "question"
- "confidence": a number from 0-100 indicating how confident you are
- "reason": a brief explanation (1 sentence) of why you chose this classification

Format: {"classification": "bug", "confidence": 85, "reason": "The issue describes something that stopped working after an update."}
Respond with ONLY valid JSON, no other text.`, truncate(title, 200), cleanBody, currentLabel)

	raw, err := l.GenerateJSON(ctx, prompt, 0.15, 1024)
	if err != nil {
		return nil, fmt.Errorf("generate classification: %w", err)
	}

	raw = extractJSONObject(raw)
	var result struct {
		Classification string `json:"classification"`
		Confidence     int    `json:"confidence"`
		Reason         string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse classification: %w", err)
	}

	// Validate classification
	validClasses := map[string]bool{"bug": true, "enhancement": true, "question": true}
	if !validClasses[result.Classification] || result.Reason == "" {
		return nil, nil
	}

	// Only surface if classification disagrees AND confidence >= 80
	if result.Classification != currentLabel && result.Confidence >= 80 {
		return &Misclassification{
			SuggestedLabel: result.Classification,
			Confidence:     result.Confidence,
			Reason:         truncate(result.Reason, 200),
		}, nil
	}

	return nil, nil
}
