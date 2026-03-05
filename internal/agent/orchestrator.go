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

func ParseApprovalSignal(comment string) ApprovalSignal {
	normalized := strings.ToLower(strings.TrimSpace(comment))

	// Check promote first (more specific)
	if strings.Contains(normalized, "publish") || strings.Contains(normalized, "promote") {
		return SignalPromote
	}
	if strings.Contains(normalized, "use as context") {
		return SignalUseAsContext
	}
	if strings.Contains(normalized, "research") {
		return SignalResearch
	}
	if strings.Contains(normalized, "revise") || strings.Contains(normalized, "needs changes") || strings.Contains(normalized, "request changes") {
		return SignalRevise
	}
	if strings.Contains(normalized, "reject") || strings.Contains(normalized, "close this") || strings.Contains(normalized, "cancel") {
		return SignalReject
	}
	if strings.Contains(normalized, "lgtm") || strings.Contains(normalized, "approve") || strings.Contains(normalized, "👍") || strings.Contains(normalized, "looks good") {
		return SignalApproved
	}
	return SignalNone
}
