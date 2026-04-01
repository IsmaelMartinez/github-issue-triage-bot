package synthesis

import (
	"encoding/json"
	"testing"
)

// lenInClassified counts items in a classified findings map by marshalling to JSON.
func lenInClassified(m map[string]any, key string) int {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return -1
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b, &items); err != nil {
		return -1
	}
	return len(items)
}

func TestClassifyFindings(t *testing.T) {
	tests := []struct {
		name         string
		findings     []Finding
		wantClusters int
		wantDrift    int
		wantUpstream int
	}{
		{
			name:     "empty findings",
			findings: []Finding{},
		},
		{
			name: "cluster type",
			findings: []Finding{
				{Type: "cluster", Severity: "warning", Title: "Auth cluster"},
			},
			wantClusters: 1,
		},
		{
			name: "drift type",
			findings: []Finding{
				{Type: "drift", Severity: "action_needed", Title: "ADR-001 drift"},
			},
			wantDrift: 1,
		},
		{
			name: "staleness maps to drift",
			findings: []Finding{
				{Type: "staleness", Severity: "warning", Title: "Stale roadmap item"},
			},
			wantDrift: 1,
		},
		{
			name: "upstream_signal type",
			findings: []Finding{
				{Type: "upstream_signal", Severity: "info", Title: "Electron v34"},
			},
			wantUpstream: 1,
		},
		{
			name: "mixed types including staleness",
			findings: []Finding{
				{Type: "cluster", Severity: "warning", Title: "Cluster A"},
				{Type: "cluster", Severity: "info", Title: "Cluster B"},
				{Type: "drift", Severity: "action_needed", Title: "Drift X"},
				{Type: "staleness", Severity: "warning", Title: "Stale Y"},
				{Type: "upstream_signal", Severity: "info", Title: "Upstream Z"},
			},
			wantClusters: 2,
			wantDrift:    2,
			wantUpstream: 1,
		},
		{
			name: "unknown type is ignored",
			findings: []Finding{
				{Type: "unknown", Severity: "info", Title: "Mystery"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFindings(tt.findings)

			// Verify required keys exist.
			for _, key := range []string{"clusters", "drift", "upstream"} {
				if _, ok := got[key]; !ok {
					t.Errorf("missing key %q in result", key)
				}
			}

			gotClusters := lenInClassified(got, "clusters")
			gotDrift := lenInClassified(got, "drift")
			gotUpstream := lenInClassified(got, "upstream")

			if gotClusters != tt.wantClusters {
				t.Errorf("clusters: got %d, want %d", gotClusters, tt.wantClusters)
			}
			if gotDrift != tt.wantDrift {
				t.Errorf("drift: got %d, want %d", gotDrift, tt.wantDrift)
			}
			if gotUpstream != tt.wantUpstream {
				t.Errorf("upstream: got %d, want %d", gotUpstream, tt.wantUpstream)
			}
		})
	}
}

func TestClassifyFindingsFieldsPreserved(t *testing.T) {
	findings := []Finding{
		{
			Type:       "cluster",
			Severity:   "warning",
			Title:      "Login issues",
			Suggestion: "investigate auth",
			Evidence:   []string{"issue #1", "issue #2"},
		},
	}
	got := classifyFindings(findings)

	b, err := json.Marshal(got["clusters"])
	if err != nil {
		t.Fatalf("marshal clusters: %v", err)
	}

	type summaryItem struct {
		Title      string   `json:"title"`
		Severity   string   `json:"severity"`
		Suggestion string   `json:"suggestion"`
		Evidence   []string `json:"evidence"`
	}
	var items []summaryItem
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatalf("unmarshal clusters: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 cluster item, got %d", len(items))
	}
	if items[0].Title != "Login issues" {
		t.Errorf("title: got %q, want %q", items[0].Title, "Login issues")
	}
	if items[0].Severity != "warning" {
		t.Errorf("severity: got %q, want %q", items[0].Severity, "warning")
	}
	if items[0].Suggestion != "investigate auth" {
		t.Errorf("suggestion: got %q, want %q", items[0].Suggestion, "investigate auth")
	}
	if len(items[0].Evidence) != 2 || items[0].Evidence[0] != "issue #1" || items[0].Evidence[1] != "issue #2" {
		t.Errorf("evidence: got %v, want [issue #1 issue #2]", items[0].Evidence)
	}
}
