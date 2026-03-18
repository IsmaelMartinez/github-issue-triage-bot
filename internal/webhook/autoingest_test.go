package webhook

import "testing"

func TestMatchesDocPaths(t *testing.T) {
	paths := []string{"docs/**", "*.md", "ADR-*"}
	tests := []struct {
		file string
		want bool
	}{
		{"docs/adr/001.md", true},
		{"docs/research/foo.md", true},
		{"README.md", true},
		{"ADR-007.md", true},
		{"src/main.go", false},
		{"internal/store/events.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			if got := matchesDocPaths(tt.file, paths); got != tt.want {
				t.Errorf("matchesDocPaths(%q) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}
