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
	return SignalNone
}
