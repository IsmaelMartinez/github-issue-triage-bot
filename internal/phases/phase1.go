package phases

import (
	"regexp"
	"strings"
)

// Phase1 analyzes a bug report body for missing information and PWA reproducibility.
// This is pure string parsing with no external dependencies.
func Phase1(body string) Phase1Result {
	var result Phase1Result

	// If the body has no form sections at all, there's nothing to analyze
	if !strings.Contains(body, "### ") {
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
	withoutMarkers := regexp.MustCompile(`(?m)^\s*\d+\.\s*`).ReplaceAllString(cleaned, "")
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
	withoutDefaults = regexp.MustCompile(`(?m)^bash\s*$`).ReplaceAllString(withoutDefaults, "")
	withoutDefaults = regexp.MustCompile(`(?m)^markdown\s*$`).ReplaceAllString(withoutDefaults, "")
	withoutDefaults = regexp.MustCompile(
		`ELECTRON_ENABLE_LOGGING=true\s+teams-for-linux\s+--logConfig='[^']*'`,
	).ReplaceAllString(withoutDefaults, "")
	withoutDefaults = strings.TrimSpace(withoutDefaults)
	return withoutDefaults == ""
}

// stripFences removes markdown code fence markers.
func stripFences(text string) string {
	re := regexp.MustCompile("`{3,}[\\w]*\n?")
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}
