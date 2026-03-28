package agent

import (
	"regexp"
	"strings"
)

type ApprovalSignal int

const (
	SignalNone ApprovalSignal = iota
	SignalApproved
	SignalRevise
	SignalReject
	SignalPromote
	SignalResearch
	SignalUseAsContext
	SignalDismiss // Maintainer dismissing the issue as not relevant/needed
)

const MaxRoundTrips = 4

// ParseApprovalSignal detects approval signals at the start of a comment.
// The signal keyword must appear at the beginning of the trimmed, lowercased
// comment to avoid false positives from natural language (e.g. "I don't think
// we need to research this" should not trigger SignalResearch).
func ParseApprovalSignal(comment string) ApprovalSignal {
	normalized := strings.ToLower(strings.TrimSpace(comment))

	// Check multi-word signals first (more specific)
	if strings.HasPrefix(normalized, "publish") || strings.HasPrefix(normalized, "promote") {
		return SignalPromote
	}
	if strings.HasPrefix(normalized, "use as context") {
		return SignalUseAsContext
	}
	if strings.HasPrefix(normalized, "needs changes") || strings.HasPrefix(normalized, "request changes") {
		return SignalRevise
	}
	if strings.HasPrefix(normalized, "close this") {
		return SignalReject
	}
	if strings.HasPrefix(normalized, "looks good") {
		return SignalApproved
	}
	// Single-word signals
	if strings.HasPrefix(normalized, "research") {
		return SignalResearch
	}
	if strings.HasPrefix(normalized, "revise") {
		return SignalRevise
	}
	if strings.HasPrefix(normalized, "reject") || strings.HasPrefix(normalized, "cancel") {
		return SignalReject
	}
	if strings.HasPrefix(normalized, "lgtm") || strings.HasPrefix(normalized, "approve") || strings.HasPrefix(normalized, "👍") {
		return SignalApproved
	}
	// Check for dismissal prefixes at the start of the comment, consistent
	// with the HasPrefix approach used for all other signals.
	if isDismissal(normalized) {
		return SignalDismiss
	}
	return SignalNone
}

// dismissalPrefixRegex matches the start of a comment against known dismissal
// patterns. Compiled once from dismissalPrefixes for efficiency. Like all other
// signals, matching is anchored to the beginning of the comment to avoid false
// positives from natural language.
var dismissalPrefixRegex = compileDismissalRegex()

// dismissalPrefixes are patterns that, when they appear at the START of a
// comment, indicate the maintainer is dismissing the issue.
var dismissalPrefixes = []string{
	"not relevant",
	"not needed",
	"not applicable",
	"not a real",
	"not a valid",
	"user error",
	"user was incorrect",
	"user was wrong",
	"already supported",
	"already exists",
	"already works",
	"no action needed",
	"no action required",
	"wontfix",
	"won't fix",
	"closing this",
	"ignore this",
	"disregard",
	"skip this",
	"this doesn't",
	"this does not",
	"this is not",
	"this isn't",
	"this was not",
	"this wasn't",
}

func compileDismissalRegex() *regexp.Regexp {
	var escaped []string
	for _, p := range dismissalPrefixes {
		escaped = append(escaped, regexp.QuoteMeta(p))
	}
	return regexp.MustCompile("^(?:" + strings.Join(escaped, "|") + ")")
}

func isDismissal(normalized string) bool {
	return dismissalPrefixRegex.MatchString(normalized)
}
