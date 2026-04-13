package safety

import (
	"fmt"
	"net/url"
	"regexp"
	"unicode"
)

var (
	mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)
	urlRe     = regexp.MustCompile(`https?://[^\s)]+`)
)

// StructuralConfig controls which deterministic checks the validator applies.
type StructuralConfig struct {
	MaxCommentLength int
	AllowedMentions  []string
	AllowedURLHosts  []string
}

// StructuralValidator enforces deterministic safety rules on content.
type StructuralValidator struct {
	config          StructuralConfig
	allowedMentions map[string]bool
	allowedHosts    map[string]bool
}

// NewStructuralValidator creates a validator from the given config.
func NewStructuralValidator(config StructuralConfig) *StructuralValidator {
	v := &StructuralValidator{config: config}

	if len(config.AllowedMentions) > 0 {
		v.allowedMentions = make(map[string]bool, len(config.AllowedMentions))
		for _, m := range config.AllowedMentions {
			v.allowedMentions[m] = true
		}
	}

	if len(config.AllowedURLHosts) > 0 {
		v.allowedHosts = make(map[string]bool, len(config.AllowedURLHosts))
		for _, h := range config.AllowedURLHosts {
			v.allowedHosts[h] = true
		}
	}

	return v
}

// AllowHost adds a hostname to the allowed URL hosts set. This is safe to call
// concurrently — duplicate additions are harmless since the map is append-only.
func (v *StructuralValidator) AllowHost(host string) {
	if host == "" {
		return
	}
	if v.allowedHosts == nil {
		v.allowedHosts = make(map[string]bool)
	}
	v.allowedHosts[host] = true
}

// Validate checks content against all configured structural rules.
// It returns on the first failure.
func (v *StructuralValidator) Validate(content string) ValidationResult {
	if v.config.MaxCommentLength > 0 && len(content) > v.config.MaxCommentLength {
		return ValidationResult{
			Passed: false,
			Reason: fmt.Sprintf("content length %d exceeds maximum %d", len(content), v.config.MaxCommentLength),
		}
	}

	for i, r := range content {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return ValidationResult{
				Passed: false,
				Reason: fmt.Sprintf("content contains control character at position %d", i),
			}
		}
	}

	if v.allowedMentions != nil {
		matches := mentionRe.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if !v.allowedMentions[m[1]] {
				return ValidationResult{
					Passed: false,
					Reason: fmt.Sprintf("mention @%s is not in the allowed list", m[1]),
				}
			}
		}
	}

	if v.allowedHosts != nil {
		urls := urlRe.FindAllString(content, -1)
		for _, raw := range urls {
			parsed, err := url.Parse(raw)
			if err != nil {
				return ValidationResult{
					Passed: false,
					Reason: fmt.Sprintf("invalid URL: %s", raw),
				}
			}
			if !v.allowedHosts[parsed.Hostname()] {
				return ValidationResult{
					Passed: false,
					Reason: fmt.Sprintf("URL host %q is not in the allowed list", parsed.Hostname()),
				}
			}
		}
	}

	return ValidationResult{Passed: true, Confidence: 1.0}
}
