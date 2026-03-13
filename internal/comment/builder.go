package comment

import (
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
)

// TriageResult holds all phase outputs for building the consolidated comment.
type TriageResult struct {
	IsBug         bool
	IsEnhancement bool
	Phase1        phases.Phase1Result
	Phase2        []phases.Suggestion
	Phase3        []phases.Duplicate
	Phase4a       []phases.ContextMatch
	Phase4b       *phases.Misclassification
}

// Build constructs the consolidated markdown comment from all phase results.
// Returns empty string if there is nothing to report.
func Build(r TriageResult) string {
	hasContent := (r.IsBug && len(r.Phase1.MissingItems) > 0) ||
		(r.IsBug && r.Phase1.IsPwaReproducible) ||
		len(r.Phase2) > 0 ||
		len(r.Phase3) > 0 ||
		len(r.Phase4a) > 0 ||
		r.Phase4b != nil

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
				parts = append(parts, fmt.Sprintf("- [%s](%s) \u2014 %s %s\n", title, url, sanitizeLLMOutput(s.Reason), sanitizeLLMOutput(s.ActionableStep)))
			} else {
				parts = append(parts, fmt.Sprintf("- %s \u2014 %s %s\n", title, sanitizeLLMOutput(s.Reason), sanitizeLLMOutput(s.ActionableStep)))
			}
		}
		parts = append(parts, "> These suggestions are based on our documentation and may not be exact matches.\n")
	}

	// Duplicate suggestions from Phase 3 (bugs only)
	if r.IsBug && len(r.Phase3) > 0 {
		openDups := filterDuplicates(r.Phase3, "open")
		closedDups := filterDuplicates(r.Phase3, "closed")

		parts = append(parts, "**This issue might be related to existing discussions:**\n")

		if len(openDups) > 0 {
			parts = append(parts, "*Potentially related open issues:*")
			for _, d := range openDups {
				parts = append(parts, fmt.Sprintf("- #%d \u2014 \"%s\" (%d%% similar) \u2014 %s", d.Number, sanitizeLLMOutput(d.Title), d.Similarity, sanitizeLLMOutput(d.Reason)))
			}
			parts = append(parts, "")
		}

		if len(closedDups) > 0 {
			parts = append(parts, "*Recently resolved issues that could be relevant:*")
			for _, d := range closedDups {
				resolvedNote := "Resolved"
				if d.Milestone != nil {
					resolvedNote = fmt.Sprintf("Resolved in %s", *d.Milestone)
				}
				parts = append(parts, fmt.Sprintf("- #%d \u2014 \"%s\" (%s) \u2014 %s", d.Number, sanitizeLLMOutput(d.Title), resolvedNote, sanitizeLLMOutput(d.Reason)))
			}
			parts = append(parts, "")
		}

		parts = append(parts, "> If one of these matches your issue, consider adding your details there instead.\n")
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

	// Misclassification hint
	if r.Phase4b != nil {
		labelHint := "a bug report"
		if r.Phase4b.SuggestedLabel == "enhancement" {
			labelHint = "a feature request"
		} else if r.Phase4b.SuggestedLabel == "question" {
			labelHint = "a question"
		}
		parts = append(parts, fmt.Sprintf(
			"> **Label suggestion:** This might be %s rather than what it's currently labelled as. %s Re-labelling as `%s` would help us apply the right triage process.\n",
			labelHint, sanitizeLLMOutput(r.Phase4b.Reason), r.Phase4b.SuggestedLabel))
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

func filterDuplicates(dups []phases.Duplicate, state string) []phases.Duplicate {
	var result []phases.Duplicate
	for _, d := range dups {
		if d.State == state {
			result = append(result, d)
		}
	}
	return result
}
