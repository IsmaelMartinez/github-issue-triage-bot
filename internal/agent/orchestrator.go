package agent

import "strings"

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
	// Check for dismissal phrases anywhere in the comment. These indicate
	// the maintainer considers the issue invalid, already handled, or not
	// worth researching. Checked after prefix signals to avoid conflicts.
	if containsDismissal(normalized) {
		return SignalDismiss
	}
	return SignalNone
}

// dismissalPhrases are substrings that indicate a maintainer is dismissing
// the issue rather than providing actionable feedback for research.
var dismissalPhrases = []string{
	"not relevant",
	"not that relevant",
	"not needed",
	"not applicable",
	"doesn't apply",
	"doesn't seem relevant",
	"doesn't seem to be",
	"user error",
	"user was incorrect",
	"user was wrong",
	"wrong url",
	"already supported",
	"already exists",
	"already works",
	"this was not needed",
	"this is not needed",
	"this isn't needed",
	"not a real",
	"not a valid",
	"closing this",
	"ignore this",
	"disregard",
	"skip this",
	"no action needed",
	"no action required",
	"wontfix",
	"won't fix",
}

func containsDismissal(normalized string) bool {
	for _, phrase := range dismissalPhrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}
