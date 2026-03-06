package agent

import "testing"

func TestParseApprovalSignal(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    ApprovalSignal
	}{
		{"lgtm lowercase", "lgtm", SignalApproved},
		{"lgtm uppercase", "LGTM", SignalApproved},
		{"approve lowercase", "approve", SignalApproved},
		{"approved lowercase", "approved", SignalApproved},
		{"approved with punctuation", "Approved!", SignalApproved},
		{"thumbs up emoji", "👍", SignalApproved},
		{"looks good", "looks good", SignalApproved},

		{"revise", "revise", SignalRevise},
		{"needs changes", "needs changes", SignalRevise},
		{"revise in sentence", "please revise this section", SignalRevise},
		{"request changes", "request changes", SignalRevise},

		{"reject", "reject", SignalReject},
		{"close this", "close this", SignalReject},
		{"cancel", "cancel", SignalReject},

		{"publish", "publish", SignalPromote},
		{"promote", "promote", SignalPromote},
		{"publish in sentence", "publish this to the public issue", SignalPromote},

		{"research", "research", SignalResearch},
		{"research in sentence", "please start the research", SignalResearch},
		{"use as context", "use as context", SignalUseAsContext},
		{"using as context", "I'll use as context", SignalUseAsContext},

		{"general feedback", "I think we should also consider performance", SignalNone},
		{"question", "What about accessibility?", SignalNone},
		{"elaboration request", "Can you elaborate on the second approach?", SignalNone},
		{"empty string", "", SignalNone},
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
