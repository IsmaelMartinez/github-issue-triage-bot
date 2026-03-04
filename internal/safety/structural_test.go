package safety

import (
	"strings"
	"testing"
)

func TestStructuralValidator(t *testing.T) {
	tests := []struct {
		name    string
		config  StructuralConfig
		content string
		passed  bool
		reason  string
	}{
		{
			name:    "clean content passes all checks",
			config:  StructuralConfig{MaxCommentLength: 1000, AllowedMentions: []string{"bot"}, AllowedURLHosts: []string{"github.com"}},
			content: "Hello @bot, see https://github.com/issue/1",
			passed:  true,
		},
		{
			name:    "content over max length fails",
			config:  StructuralConfig{MaxCommentLength: 10},
			content: strings.Repeat("a", 11),
			passed:  false,
			reason:  "exceeds maximum",
		},
		{
			name:    "content at max length passes",
			config:  StructuralConfig{MaxCommentLength: 10},
			content: strings.Repeat("a", 10),
			passed:  true,
		},
		{
			name:    "control character fails",
			config:  StructuralConfig{},
			content: "hello\x00world",
			passed:  false,
			reason:  "control character",
		},
		{
			name:    "newline tab carriage return allowed",
			config:  StructuralConfig{},
			content: "hello\nworld\ttab\rreturn",
			passed:  true,
		},
		{
			name:    "allowed mention passes",
			config:  StructuralConfig{AllowedMentions: []string{"bot", "admin"}},
			content: "cc @bot @admin",
			passed:  true,
		},
		{
			name:    "disallowed mention fails",
			config:  StructuralConfig{AllowedMentions: []string{"bot"}},
			content: "cc @hacker",
			passed:  false,
			reason:  "@hacker is not in the allowed list",
		},
		{
			name:    "no mentions passes with allowed list",
			config:  StructuralConfig{AllowedMentions: []string{"bot"}},
			content: "no mentions here",
			passed:  true,
		},
		{
			name:    "mentions unchecked when no allowed list",
			config:  StructuralConfig{},
			content: "@anyone can be mentioned",
			passed:  true,
		},
		{
			name:    "allowed URL host passes",
			config:  StructuralConfig{AllowedURLHosts: []string{"github.com"}},
			content: "see https://github.com/issue/1",
			passed:  true,
		},
		{
			name:    "disallowed URL host fails",
			config:  StructuralConfig{AllowedURLHosts: []string{"github.com"}},
			content: "see https://evil.com/payload",
			passed:  false,
			reason:  `"evil.com" is not in the allowed list`,
		},
		{
			name:    "no URLs passes with allowed list",
			config:  StructuralConfig{AllowedURLHosts: []string{"github.com"}},
			content: "no links here",
			passed:  true,
		},
		{
			name:    "URLs unchecked when no allowed list",
			config:  StructuralConfig{},
			content: "see https://anything.com",
			passed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewStructuralValidator(tt.config)
			result := v.Validate(tt.content)
			if result.Passed != tt.passed {
				t.Fatalf("Passed = %v, want %v (reason: %s)", result.Passed, tt.passed, result.Reason)
			}
			if !tt.passed && !strings.Contains(result.Reason, tt.reason) {
				t.Fatalf("Reason = %q, want it to contain %q", result.Reason, tt.reason)
			}
			if tt.passed && result.Confidence != 1.0 {
				t.Fatalf("Confidence = %v, want 1.0", result.Confidence)
			}
		})
	}
}
