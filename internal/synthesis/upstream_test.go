package synthesis

import (
	"testing"
)

func TestUpstreamSynthesizerName(t *testing.T) {
	s := NewUpstreamSynthesizer(nil)
	if got := s.Name(); got != "upstream_impact" {
		t.Errorf("Name() = %q, want %q", got, "upstream_impact")
	}
}

func TestIsDeferredOrRejected(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty content", "", false},
		{"normal content", "This ADR describes the chosen approach for caching.", false},
		{"contains deferred", "Status: Deferred pending further analysis.", true},
		{"contains rejected", "This proposal was rejected in favour of alternative B.", true},
		{"case insensitive deferred", "We DEFERRED this decision until Q3.", true},
		{"case insensitive rejected", "REJECTED by the team.", true},
		{"both present", "Initially deferred, then rejected.", true},
		{"substring match", "The undeferred items were processed.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeferredOrRejected(tt.content); got != tt.want {
				t.Errorf("isDeferredOrRejected(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestUpstreamFindingConstruction(t *testing.T) {
	f := Finding{
		Type:       "upstream_signal",
		Severity:   "info",
		Title:      `Upstream change "Electron v40" may impact "ADR-007"`,
		Evidence:   []string{"upstream: Electron v40 (type: upstream_release)", "related: ADR-007 (type: adr, distance: 0.180)"},
		Suggestion: `Review "ADR-007" in light of upstream change "Electron v40".`,
	}

	if f.Type != "upstream_signal" {
		t.Errorf("Type = %q, want %q", f.Type, "upstream_signal")
	}
	if f.Severity != "info" {
		t.Errorf("Severity = %q, want %q", f.Severity, "info")
	}
	if len(f.Evidence) != 2 {
		t.Errorf("Evidence length = %d, want 2", len(f.Evidence))
	}
	if f.Suggestion == "" {
		t.Error("Suggestion should not be empty")
	}

	// Verify action_needed severity for deferred content
	fAction := Finding{
		Type:     "upstream_signal",
		Severity: "action_needed",
		Title:    `Upstream change "Electron v40" may impact "ADR-009 (deferred)"`,
	}
	if fAction.Severity != "action_needed" {
		t.Errorf("Severity = %q, want %q", fAction.Severity, "action_needed")
	}
}
