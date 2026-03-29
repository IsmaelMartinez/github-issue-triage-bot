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
	IsDocBug      bool // documentation/meta bug — skip PWA note and debug log prompt
	Phase1        phases.Phase1Result
	Phase2        []phases.Suggestion
	Phase4a       []phases.ContextMatch
}

// Build constructs the consolidated markdown comment from all phase results.
// Returns empty string if there is nothing to report.
func Build(r TriageResult) string {
	hasPwaNote := r.IsBug && !r.IsDocBug && r.Phase1.IsPwaReproducible
	missingCount := countRelevantMissing(r)
	hasContent := (r.IsBug && missingCount > 0) ||
		hasPwaNote ||
		len(r.Phase2) > 0 ||
		len(r.Phase4a) > 0

	if !hasContent {
		return ""
	}

	var parts []string

	// PWA reproducibility note (bugs only, skip for documentation bugs)
	if r.IsBug && !r.IsDocBug && r.Phase1.IsPwaReproducible {
		parts = append(parts,
			"> This bug also occurs on the [Teams web app](https://teams.microsoft.com), "+
				"which suggests a Microsoft-side issue. Consider reporting to the "+
				"[Microsoft Feedback Portal](https://feedbackportal.microsoft.com/) too. "+
				"We'll still take a look.\n")
	}

	// Known issue matches from Phase 2 (bugs only)
	if r.IsBug && len(r.Phase2) > 0 {
		parts = append(parts, "**Possibly related:**\n")
		for _, s := range r.Phase2 {
			url := sanitizeURL(s.DocURL)
			title := sanitizeLLMOutput(s.Title)
			if url != "" {
				parts = append(parts, fmt.Sprintf("- [%s](%s) \u2014 %s", title, url, sanitizeLLMOutput(s.Reason)))
			} else {
				parts = append(parts, fmt.Sprintf("- %s \u2014 %s", title, sanitizeLLMOutput(s.Reason)))
			}
		}
		parts = append(parts, "")
	}

	// Enhancement context from Phase 4a (enhancements only)
	if r.IsEnhancement && len(r.Phase4a) > 0 {
		statusLabels := map[string]string{
			"shipped":       "Shipped",
			"planned":       "Planned",
			"investigating": "Investigating",
			"deferred":      "Deferred",
			"rejected":      "Explored",
		}

		parts = append(parts, "**Related work:**\n")
		for _, ctx := range r.Phase4a {
			statusLabel := statusLabels[ctx.Status]
			if statusLabel == "" {
				statusLabel = ctx.Status
			}
			sourceLabel := "Roadmap"
			if ctx.Source == "adr" {
				sourceLabel = "ADR"
			} else if ctx.Source == "research" {
				sourceLabel = "Research"
			}

			url := sanitizeURL(ctx.DocURL)
			topic := sanitizeLLMOutput(ctx.Topic)
			topicLink := topic
			if url != "" {
				topicLink = fmt.Sprintf("[%s](%s)", topic, url)
			}
			if ctx.IsInfeasible {
				parts = append(parts, fmt.Sprintf("- %s (%s) \u2014 %s",
					topicLink, sourceLabel, sanitizeLLMOutput(ctx.Reason)))
			} else {
				parts = append(parts, fmt.Sprintf("- %s (%s, %s) \u2014 %s",
					topicLink, sourceLabel, statusLabel, sanitizeLLMOutput(ctx.Reason)))
			}
		}
		parts = append(parts, "")
	}

	// Missing information checklist and debug instructions (bugs only, single pass).
	// For documentation bugs, skip debug logs and PWA reproducibility — they're irrelevant.
	if r.IsBug && len(r.Phase1.MissingItems) > 0 {
		var displayItems []phases.MissingItem
		debugMissing := false
		for _, item := range r.Phase1.MissingItems {
			if item.Label == "Debug console output" {
				debugMissing = true
			}
			if r.IsDocBug && (item.Label == "Debug console output" || item.Label == "PWA reproducibility") {
				continue
			}
			displayItems = append(displayItems, item)
		}
		if len(displayItems) > 0 {
			parts = append(parts, "**Missing information:**")
			for _, item := range displayItems {
				parts = append(parts, fmt.Sprintf("- [ ] **%s** \u2014 %s", item.Label, item.Detail))
			}
			parts = append(parts, "")
		}
		if !r.IsDocBug && debugMissing {
			parts = append(parts,
				"<details>\n"+
					"<summary>How to get debug logs</summary>\n\n"+
					"```bash\n"+
					"ELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{\"transports\":{\"console\":{\"level\":\"debug\"}}}'\n"+
					"```\n"+
					"Reproduce the issue and copy the relevant output.\n\n"+
					"</details>\n")
		}
	}

	// Footer: tip + feedback + bot disclosure
	if r.IsBug {
		parts = append(parts,
			"*Bot suggestion \u2014 [Troubleshooting Guide](https://ismaelmartinez.github.io/teams-for-linux/troubleshooting) \u2014 "+
				"react \U0001F44D/\U0001F44E or [share feedback](https://github.com/IsmaelMartinez/github-issue-triage-bot/issues/new?template=bot-feedback.yml).*")
	} else {
		parts = append(parts,
			"*Bot suggestion \u2014 [Roadmap](https://ismaelmartinez.github.io/teams-for-linux/development/plan/roadmap) \u2014 "+
				"react \U0001F44D/\U0001F44E or [share feedback](https://github.com/IsmaelMartinez/github-issue-triage-bot/issues/new?template=bot-feedback.yml).*")
	}

	return strings.Join(parts, "\n")
}

// countRelevantMissing returns the number of missing items that will actually
// be displayed, accounting for doc-bug filtering.
func countRelevantMissing(r TriageResult) int {
	count := 0
	for _, item := range r.Phase1.MissingItems {
		if r.IsDocBug && (item.Label == "Debug console output" || item.Label == "PWA reproducibility") {
			continue
		}
		count++
	}
	return count
}
