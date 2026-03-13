package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
)

// Phase4b detects misclassified issues using LLM classification.
// No vector search needed — just sends the issue to the LLM for classification.
func Phase4b(ctx context.Context, l llm.Provider, logger *slog.Logger, title, body, currentLabel string) (*Misclassification, error) {
	logger.Info("phase4b start")
	cleanBody := stripCodeFences(body, 1500)

	systemPrompt := `You are a classification assistant for the "Teams for Linux" open source project.

Classify this GitHub issue as one of: bug, enhancement, or question.

Classification rules:
- "bug": Something that used to work and broke, a crash, an error, unexpected behavior, a regression
- "enhancement": A new feature request, an improvement to existing functionality, a UI change suggestion
- "question": A how-to question, a request for help or documentation

Return a JSON object with:
- "classification": one of "bug", "enhancement", "question"
- "confidence": a number from 0-100 indicating how confident you are
- "reason": a brief explanation (1 sentence) of why you chose this classification

Format: {"classification": "bug", "confidence": 85, "reason": "The issue describes something that stopped working after an update."}
Respond with ONLY valid JSON, no other text.`
	userContent := fmt.Sprintf("ISSUE:\nTitle: %s\nBody: %s\n\nCurrent label: %s",
		truncate(title, 200), cleanBody, currentLabel)

	raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.15, 8192)
	if err != nil {
		return nil, fmt.Errorf("generate classification: %w", err)
	}

	raw = ExtractJSONObject(raw)
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
		logger.Info("phase4b finish", "result", "invalid_classification")
		return nil, nil
	}

	// Only surface if classification disagrees AND confidence >= 80
	if result.Classification != currentLabel && result.Confidence >= 80 {
		logger.Info("phase4b finish", "result", "misclassified", "suggested", result.Classification, "confidence", result.Confidence)
		return &Misclassification{
			SuggestedLabel: result.Classification,
			Confidence:     result.Confidence,
			Reason:         truncate(result.Reason, 200),
		}, nil
	}

	logger.Info("phase4b finish", "result", "correct_classification")
	return nil, nil
}
