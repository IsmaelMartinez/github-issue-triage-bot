package agent

import "testing"

func TestParseApprovalSignal(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    ApprovalSignal
	}{
		// Approved signals
		{"lgtm lowercase", "lgtm", SignalApproved},
		{"lgtm uppercase", "LGTM", SignalApproved},
		{"approve lowercase", "approve", SignalApproved},
		{"approved lowercase", "approved", SignalApproved},
		{"approved with punctuation", "Approved!", SignalApproved},
		{"thumbs up emoji", "👍", SignalApproved},
		{"looks good", "looks good", SignalApproved},
		{"looks good with detail", "looks good to me, ship it", SignalApproved},
		{"lgtm with whitespace", "  LGTM  ", SignalApproved},

		// Revise signals
		{"revise", "revise", SignalRevise},
		{"needs changes", "needs changes", SignalRevise},
		{"revise with feedback", "revise: please focus on CSS approaches", SignalRevise},
		{"request changes", "request changes", SignalRevise},

		// Reject signals
		{"reject", "reject", SignalReject},
		{"close this", "close this", SignalReject},
		{"cancel", "cancel", SignalReject},

		// Promote signals
		{"publish", "publish", SignalPromote},
		{"promote", "promote", SignalPromote},
		{"publish with context", "publish this to the public issue", SignalPromote},

		// Research signals
		{"research", "research", SignalResearch},
		{"research with whitespace", "  research  ", SignalResearch},

		// Use as context
		{"use as context", "use as context", SignalUseAsContext},

		// Non-signals (general feedback)
		{"general feedback", "I think we should also consider performance", SignalNone},
		{"question", "What about accessibility?", SignalNone},
		{"elaboration request", "Can you elaborate on the second approach?", SignalNone},
		{"empty string", "", SignalNone},

		// False positive prevention: signal keywords embedded in natural language
		{"research in sentence", "I don't think we need to research this", SignalNone},
		{"reject in sentence", "I would reject the premise that this is needed", SignalNone},
		{"approve in sentence", "I can't approve this without more context", SignalNone},
		{"cancel in sentence", "don't cancel the meeting", SignalNone},
		{"revise in sentence", "please don't revise the whole thing", SignalNone},
		{"retry contains research", "retry", SignalNone},
		{"natural language with issues", "we wrap around the web version. I don't think the issues 27, 1606 and 1575 are related.", SignalNone},
		{"thumbs up in longer comment", "👍 but also consider the edge cases", SignalApproved},
		{"publish in longer text", "don't publish yet", SignalNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseApprovalSignal(tt.comment)
			if got != tt.want {
				t.Errorf("ParseApprovalSignal(%q) = %d, want %d", tt.comment, got, tt.want)
			}
		})
	}
}
