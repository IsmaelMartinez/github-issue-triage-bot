package store

import "testing"

func TestExtractReferences(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []DocReference
	}{
		{
			name:    "issue references",
			content: "See #42 and #123 for details",
			want: []DocReference{
				{TargetType: "issue", TargetID: "#42", Relationship: "references"},
				{TargetType: "issue", TargetID: "#123", Relationship: "references"},
			},
		},
		{
			name:    "ADR references",
			content: "As decided in ADR-007, we use WebRTC. See also ADR-12.",
			want: []DocReference{
				{TargetType: "document", TargetID: "ADR-007", Relationship: "references"},
				{TargetType: "document", TargetID: "ADR-12", Relationship: "references"},
			},
		},
		{
			name:    "no references",
			content: "Just some plain text with no refs",
			want:    nil,
		},
		{
			name:    "mixed references",
			content: "See #42, related to ADR-003",
			want: []DocReference{
				{TargetType: "issue", TargetID: "#42", Relationship: "references"},
				{TargetType: "document", TargetID: "ADR-003", Relationship: "references"},
			},
		},
		{
			name:    "deduplicates same reference",
			content: "See #42 and again #42",
			want: []DocReference{
				{TargetType: "issue", TargetID: "#42", Relationship: "references"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractReferences(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractReferences() returned %d refs, want %d", len(got), len(tt.want))
			}
			for i, ref := range got {
				if ref.TargetType != tt.want[i].TargetType || ref.TargetID != tt.want[i].TargetID {
					t.Errorf("ref[%d] = {%s, %s}, want {%s, %s}", i, ref.TargetType, ref.TargetID, tt.want[i].TargetType, tt.want[i].TargetID)
				}
			}
		})
	}
}
