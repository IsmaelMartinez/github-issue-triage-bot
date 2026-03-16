package comment

import (
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
)

// TriageResult holds all phase outputs for building the consolidated comment.
// Phase 3 (duplicate detection) and Phase 4b (misclassification) were removed
// in favour of GitHub native tooling. See docs/plans/2026-03-15-lean-bot-pivot-design.md.
type TriageResult struct {
	IsBug         bool
	IsEnhancement bool
	Phase1        phases.Phase1Result
	Phase2        []phases.Suggestion
	Phase4a       []phases.ContextMatch
}

// Build constructs the consolidated markdown comment from all phase results.
// Returns empty string if there is nothing to report.
func Build(r TriageResult) string {
	hasContent := (r.IsBug && len(r.Phase1.MissingItems) > 0) ||
		(r.IsBug && r.Phase1.IsPwaReproducible) ||
		len(r.Phase2) > 0 ||
		len(r.Phase4a) > 0

	if !hasContent {
		return ""
	}

	var parts []string

	// Greeting
	if r.IsBug {
		parts = append(parts, "\U0001F44B Thanks for reporting this issue!\n")
	} else {
		parts = append(parts, "\U0001F44B Thanks for the feature suggestion!\n")
	}

	// --- Bug-specific sections ---

	// PWA reproducibility note (bugs only)
	if r.IsBug && r.Phase1.IsPwaReproducible {
		parts = append(parts,
			"> **Note:** You mentioned this bug also occurs on the [Microsoft Teams web app](https://teams.microsoft.com). "+
				"This suggests the issue may be with Microsoft Teams itself rather than Teams for Linux. "+
				"You could also report it to [Microsoft Feedback Portal](https://feedbackportal.microsoft.com/). "+
				"That said, we'll still take a look \u2014 there may be something we can do on our end.\n")
	}

	// Solution suggestions from Phase 2 (bugs only)
	if r.IsBug && len(r.Phase2) > 0 {
		parts = append(parts, "**This might be related to a known issue:**\n")
		for _, s := range r.Phase2 {
			url := sanitizeURL(s.DocURL)
			title := sanitizeLLMOutput(s.Title)
			if url != "" {
				parts = append(parts, fmt.Sprintf("- [%s](%s) \u2014 %s\n", title, url, sanitizeLLMOutput(s.Reason)))
			} else {
				parts = append(parts, fmt.Sprintf("- %s \u2014 %s\n", title, sanitizeLLMOutput(s.Reason)))
			}
		}
		parts = append(parts, "> These suggestions are based on our documentation and may not be exact matches.\n")
	}

	// --- Enhancement-specific sections ---

	// Enhancement context from Phase 4a (enhancements only)
	if r.IsEnhancement && len(r.Phase4a) > 0 {
		statusLabels := map[string]string{
			"shipped":       "Shipped",
			"planned":       "Planned",
			"investigating": "Under investigation",
			"deferred":      "Deferred / awaiting feedback",
			"rejected":      "Previously explored",
		}

		parts = append(parts, "**We've previously explored related areas:**\n")
		for _, ctx := range r.Phase4a {
			statusLabel := statusLabels[ctx.Status]
			if statusLabel == "" {
				statusLabel = ctx.Status
			}
			sourceLabel := "Roadmap"
			if ctx.Source == "adr" {
				sourceLabel = "Architecture Decision"
			} else if ctx.Source == "research" {
				sourceLabel = "Research"
			}
			updatedNote := ""
			if ctx.LastUpdated != nil {
				updatedNote = fmt.Sprintf(" (last updated: %s)", *ctx.LastUpdated)
			}

			url := sanitizeURL(ctx.DocURL)
			topic := sanitizeLLMOutput(ctx.Topic)
			topicLink := topic
			if url != "" {
				topicLink = fmt.Sprintf("[%s](%s)", topic, url)
			}
			if ctx.IsInfeasible {
				parts = append(parts, fmt.Sprintf("- %s (%s) \u2014 We explored this and documented our findings.%s %s",
					topicLink, sourceLabel, updatedNote, sanitizeLLMOutput(ctx.Reason)))
			} else {
				parts = append(parts, fmt.Sprintf("- %s (%s, %s)%s \u2014 %s",
					topicLink, sourceLabel, statusLabel, updatedNote, sanitizeLLMOutput(ctx.Reason)))
			}
		}
		parts = append(parts, "")
		parts = append(parts, "> Our roadmap is a living document and priorities may have changed. Your feedback helps shape future development.\n")
	}

	// --- Common sections ---

	// Missing information checklist (bugs only — Phase 1 checks bug template fields)
	if r.IsBug && len(r.Phase1.MissingItems) > 0 {
		parts = append(parts, "To help us investigate, could you provide some additional details?\n")
		parts = append(parts, "**Missing information:**")
		for _, item := range r.Phase1.MissingItems {
			parts = append(parts, fmt.Sprintf("- [ ] **%s** \u2014 %s", item.Label, item.Detail))
		}
		parts = append(parts, "")
	}

	// Debug instructions (collapsible)
	for _, item := range r.Phase1.MissingItems {
		if item.Label == "Debug console output" {
			parts = append(parts,
				"<details>\n"+
					"<summary><b>How to get debug logs</b></summary>\n\n"+
					"1. Run the application from the terminal with logging enabled:\n"+
					"   ```bash\n"+
					"   ELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{\"transports\":{\"console\":{\"level\":\"debug\"}}}'\n"+
					"   ```\n"+
					"2. Reproduce the issue\n"+
					"3. Copy the relevant console output\n"+
					"4. Feel free to redact any sensitive information (emails, URLs, etc.)\n\n"+
					"</details>\n")
			break
		}
	}

	// Tip link
	if r.IsBug {
		parts = append(parts,
			"> **Tip:** You might also find helpful information in our "+
				"[Troubleshooting Guide](https://ismaelmartinez.github.io/teams-for-linux/troubleshooting).\n")
	} else {
		parts = append(parts,
			"> **Tip:** Check our [Development Roadmap](https://ismaelmartinez.github.io/teams-for-linux/development/plan/roadmap) "+
				"for the current project direction and planned features.\n")
	}

	// Bot disclosure
	parts = append(parts, "---\n")
	parts = append(parts,
		"*I'm a bot that helps with issue triage. "+
			"Suggestions are based on documentation and may not be exact. "+
			"A maintainer will review this issue.*")

	return strings.Join(parts, "\n")
}

