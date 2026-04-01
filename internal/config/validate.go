package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validFrequencies lists accepted synthesis frequency values.
var validFrequencies = map[string]bool{
	"weekly":  true,
	"monthly": true,
}

// validDays lists accepted day-of-week values for synthesis scheduling.
var validDays = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
}

// Validate checks a ButlerConfig for common misconfigurations and returns
// a list of warnings. An empty list means the config is valid.
func (c ButlerConfig) Validate() []string {
	var warnings []string

	// Shadow repo required when synthesis is enabled
	if c.Capabilities.Synthesis && c.ShadowRepo == "" {
		warnings = append(warnings, "synthesis is enabled but shadow_repo is not set — briefings have nowhere to post")
	}

	// Synthesis frequency must be a known value
	if c.Capabilities.Synthesis && c.Synthesis.Frequency != "" && !validFrequencies[c.Synthesis.Frequency] {
		warnings = append(warnings, fmt.Sprintf("synthesis frequency %q is not recognised (use weekly or monthly)", c.Synthesis.Frequency))
	}

	// Synthesis day must be a valid day of week
	if c.Capabilities.Synthesis && c.Synthesis.Day != "" && !validDays[strings.ToLower(c.Synthesis.Day)] {
		warnings = append(warnings, fmt.Sprintf("synthesis day %q is not a valid day of the week", c.Synthesis.Day))
	}

	// Thresholds must be in 0.0-1.0 range
	for name, val := range c.Thresholds {
		if val < 0 || val > 1 {
			warnings = append(warnings, fmt.Sprintf("threshold %q value %.2f is outside the 0.0-1.0 range", name, val))
		}
	}

	// DocPaths must be valid glob patterns
	for _, pattern := range c.DocPaths {
		if _, err := filepath.Match(pattern, "test"); err != nil {
			warnings = append(warnings, fmt.Sprintf("doc_path %q is not a valid glob pattern: %v", pattern, err))
		}
	}

	// MaxDailyLLMCalls sanity check
	if c.MaxDailyLLMCalls > 250 {
		warnings = append(warnings, fmt.Sprintf("max_daily_llm_calls %d exceeds the Gemini free tier limit of 250", c.MaxDailyLLMCalls))
	}

	return warnings
}
