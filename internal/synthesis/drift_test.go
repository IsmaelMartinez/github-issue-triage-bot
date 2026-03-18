package synthesis

import (
	"testing"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestIsStale(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		updated  time.Time
		cutoff   time.Time
		wantStale bool
	}{
		{
			name:      "document updated before cutoff is stale",
			updated:   now.Add(-48 * time.Hour),
			cutoff:    now.Add(-24 * time.Hour),
			wantStale: true,
		},
		{
			name:      "document updated after cutoff is not stale",
			updated:   now.Add(-12 * time.Hour),
			cutoff:    now.Add(-24 * time.Hour),
			wantStale: false,
		},
		{
			name:      "document updated exactly at cutoff is not stale",
			updated:   now.Add(-24 * time.Hour),
			cutoff:    now.Add(-24 * time.Hour),
			wantStale: false,
		},
		{
			name:      "very old document is stale",
			updated:   now.Add(-365 * 24 * time.Hour),
			cutoff:    now.Add(-7 * 24 * time.Hour),
			wantStale: true,
		},
		{
			name:      "just-updated document is not stale",
			updated:   now,
			cutoff:    now.Add(-1 * time.Hour),
			wantStale: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := store.Document{
				ID:        1,
				Title:     "Test ADR",
				DocType:   "adr",
				UpdatedAt: tt.updated,
			}
			got := isStale(doc, tt.cutoff)
			if got != tt.wantStale {
				t.Errorf("isStale() = %v, want %v (updated=%v, cutoff=%v)",
					got, tt.wantStale, tt.updated, tt.cutoff)
			}
		})
	}
}

func TestDriftSynthesizerName(t *testing.T) {
	ds := NewDriftSynthesizer(nil)
	if ds.Name() != "drift_detection" {
		t.Errorf("Name() = %q, want %q", ds.Name(), "drift_detection")
	}
}

func TestExtractAreas(t *testing.T) {
	tests := []struct {
		name     string
		event    store.RepoEvent
		expected []string
	}{
		{
			name: "uses Areas field when present",
			event: store.RepoEvent{
				Areas: []string{"auth", "webhook"},
			},
			expected: []string{"auth", "webhook"},
		},
		{
			name: "extracts from changed_files as []any",
			event: store.RepoEvent{
				Metadata: map[string]any{
					"changed_files": []any{
						"internal/phases/phase1.go",
						"internal/phases/phase2.go",
						"cmd/server/main.go",
					},
				},
			},
			expected: []string{"internal", "cmd"},
		},
		{
			name: "extracts from changed_files as comma-separated string",
			event: store.RepoEvent{
				Metadata: map[string]any{
					"changed_files": "internal/store/postgres.go,docs/readme.md",
				},
			},
			expected: []string{"internal", "docs"},
		},
		{
			name: "returns nil when no areas and no metadata",
			event: store.RepoEvent{
				Metadata: map[string]any{},
			},
			expected: nil,
		},
		{
			name: "returns nil when metadata is nil",
			event: store.RepoEvent{},
			expected: nil,
		},
		{
			name: "deduplicates areas from changed_files",
			event: store.RepoEvent{
				Metadata: map[string]any{
					"changed_files": []any{
						"internal/phases/phase1.go",
						"internal/store/postgres.go",
					},
				},
			},
			expected: []string{"internal"},
		},
		{
			name: "handles root-level files",
			event: store.RepoEvent{
				Metadata: map[string]any{
					"changed_files": []any{"go.mod", "go.sum"},
				},
			},
			expected: []string{"go.mod", "go.sum"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAreas(tt.event)
			if !stringSliceEqual(got, tt.expected) {
				t.Errorf("extractAreas() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractAreaFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"internal/phases/phase1.go", "internal"},
		{"cmd/server/main.go", "cmd"},
		{"go.mod", "go.mod"},
		{"/internal/store/postgres.go", "internal"},
		{"docs/readme.md", "docs"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractAreaFromPath(tt.path)
			if got != tt.want {
				t.Errorf("extractAreaFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildADRAreaIndex(t *testing.T) {
	adrs := []store.Document{
		{ID: 1, Title: "ADR-001 Use Gemini as LLM", DocType: "adr"},
		{ID: 2, Title: "ADR-002 Authentication flow", DocType: "adr", Metadata: map[string]any{
			"areas": []any{"auth", "security"},
		}},
	}

	index := buildADRAreaIndex(adrs)

	// Check that title words are indexed.
	if _, ok := index["gemini"]; !ok {
		t.Error("expected 'gemini' in index from ADR-001 title")
	}
	if _, ok := index["authentication"]; !ok {
		t.Error("expected 'authentication' in index from ADR-002 title")
	}
	// Check metadata areas are indexed.
	if _, ok := index["auth"]; !ok {
		t.Error("expected 'auth' in index from ADR-002 metadata")
	}
	if _, ok := index["security"]; !ok {
		t.Error("expected 'security' in index from ADR-002 metadata")
	}
	// Short tokens (<=2 chars) should be excluded.
	if _, ok := index["as"]; ok {
		t.Error("'as' should be excluded (<=2 chars)")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"ADR-001 Use Gemini", []string{"ADR", "001", "Use", "Gemini"}},
		{"auth/webhook", []string{"auth", "webhook"}},
		{"config_parser", []string{"config", "parser"}},
		{"simple", []string{"simple"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenize(tt.input)
			if !stringSliceEqual(got, tt.want) {
				t.Errorf("tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeduplicate(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{"x"}, []string{"x"}},
		{nil, []string{}},
	}

	for _, tt := range tests {
		got := deduplicate(tt.input)
		if !stringSliceEqual(got, tt.want) {
			t.Errorf("deduplicate(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// stringSliceEqual compares two string slices for equality.
func stringSliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
