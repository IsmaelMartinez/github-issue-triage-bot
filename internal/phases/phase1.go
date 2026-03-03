package phases

import (
	"regexp"
	"strings"
)

// Pre-compiled regexes for known issue template section headers.
var (
	sectionReproSteps    = regexp.MustCompile(`### Reproduction steps\s*\n([\s\S]*?)(?:\n### |$)`)
	sectionExpected      = regexp.MustCompile(`### Expected Behavior\s*\n([\s\S]*?)(?:\n### |$)`)
	sectionDebug         = regexp.MustCompile(`### Debug\s*\n([\s\S]*?)(?:\n### |$)`)
	sectionCanReproduce  = regexp.MustCompile(`### Can you reproduce this bug on the Microsoft Teams web app \(https://teams\.microsoft\.com\)\?\s*\n([\s\S]*?)(?:\n### |$)`)
	reNumberedMarkers    = regexp.MustCompile(`(?m)^\s*\d+\.\s*`)
	reDebugBash          = regexp.MustCompile(`(?m)^bash\s*$`)
	reDebugMarkdown      = regexp.MustCompile(`(?m)^markdown\s*$`)
	reDebugElectron      = regexp.MustCompile(`ELECTRON_ENABLE_LOGGING=true\s+teams-for-linux\s+--logConfig='[^']*'`)
	reStripFences        = regexp.MustCompile("`{3,}[\\w]*\n?")
)

// sectionRegexes maps header names to their pre-compiled regex for getSection fallback.
var sectionRegexes = map[string]*regexp.Regexp{
	"Reproduction steps": sectionReproSteps,
	"Expected Behavior":  sectionExpected,
	"Debug":              sectionDebug,
	"Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?": sectionCanReproduce,
}

// Phase1 analyzes a bug report body for missing information and PWA reproducibility.
// This is pure string parsing with no external dependencies.
func Phase1(body string) Phase1Result {
	var result Phase1Result

	// Empty or whitespace-only bodies have nothing to analyze.
	if strings.TrimSpace(body) == "" {
		return result
	}

	// If the body has content but no form sections, all template fields are missing.
	if !strings.Contains(body, "### ") {
		result.MissingItems = []MissingItem{
			{Label: "Reproduction steps", Detail: "Step-by-step instructions to trigger the bug (the more specific, the faster we can investigate)"},
			{Label: "Debug console output", Detail: "Log output from running the application with debug logging enabled"},
			{Label: "Expected behavior", Detail: "A description of what you expected to happen instead"},
			{Label: "PWA reproducibility", Detail: "Whether the issue also occurs on the Microsoft Teams web app"},
		}
		return result
	}

	reproSteps := getSection(body, "Reproduction steps")
	expectedBehavior := getSection(body, "Expected Behavior")
	debugOutput := getSection(body, "Debug")
	canReproduce := getSection(body, "Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?")

	if isDefaultStepsTemplate(reproSteps) {
		result.MissingItems = append(result.MissingItems, MissingItem{
			Label:  "Reproduction steps",
			Detail: "Step-by-step instructions to trigger the bug (the more specific, the faster we can investigate)",
		})
	}

	if isDebugMissing(debugOutput) {
		result.MissingItems = append(result.MissingItems, MissingItem{
			Label:  "Debug console output",
			Detail: "Log output from running the application with debug logging enabled",
		})
	}

	if isDefaultStepsTemplate(expectedBehavior) {
		result.MissingItems = append(result.MissingItems, MissingItem{
			Label:  "Expected behavior",
			Detail: "A description of what you expected to happen instead",
		})
	}

	result.IsPwaReproducible = strings.Contains(strings.ToLower(canReproduce), "yes")

	return result
}

// getSection extracts the content under a ### header in a GitHub issue form body.
func getSection(body, header string) string {
	if re, ok := sectionRegexes[header]; ok {
		match := re.FindStringSubmatch(body)
		if len(match) < 2 {
			return ""
		}
		return strings.TrimSpace(match[1])
	}
	// Fallback for unknown headers: compile on the fly.
	escaped := regexp.QuoteMeta(header)
	re := regexp.MustCompile(`### ` + escaped + `\s*\n([\s\S]*?)(?:\n### |$)`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

// isDefaultStepsTemplate checks if the content is empty or contains only the default template placeholders.
func isDefaultStepsTemplate(content string) bool {
	if content == "" || content == "_No response_" {
		return true
	}
	cleaned := stripFences(content)
	if cleaned == "" {
		return true
	}
	// Remove numbered list markers and ellipsis, then check if anything remains
	withoutMarkers := reNumberedMarkers.ReplaceAllString(cleaned, "")
	withoutMarkers = strings.NewReplacer("...", "").Replace(withoutMarkers)
	withoutMarkers = strings.TrimSpace(withoutMarkers)
	return withoutMarkers == ""
}

// isDebugMissing checks if the debug output is empty or contains only the default template.
func isDebugMissing(content string) bool {
	if content == "" || content == "_No response_" {
		return true
	}
	cleaned := stripFences(content)
	if cleaned == "" {
		return true
	}
	// Remove known defaults from the template
	withoutDefaults := cleaned
	withoutDefaults = reDebugBash.ReplaceAllString(withoutDefaults, "")
	withoutDefaults = reDebugMarkdown.ReplaceAllString(withoutDefaults, "")
	withoutDefaults = reDebugElectron.ReplaceAllString(withoutDefaults, "")
	withoutDefaults = strings.TrimSpace(withoutDefaults)
	return withoutDefaults == ""
}

// stripFences removes markdown code fence markers.
func stripFences(text string) string {
	return strings.TrimSpace(reStripFences.ReplaceAllString(text, ""))
}
