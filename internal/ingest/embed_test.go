package ingest

import "testing"

func TestDocFromRawContent(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		path    string
		content string
		want    string
	}{
		{"ADR file", "owner/repo", "docs/adr/001-foo.md", "# ADR 001", "adr"},
		{"research file", "owner/repo", "docs/research/bar.md", "# Research", "research"},
		{"roadmap file", "owner/repo", "docs/plan/roadmap.md", "# Roadmap", "roadmap"},
		{"troubleshooting", "owner/repo", "docs/troubleshooting/fix.md", "# Fix", "troubleshooting"},
		{"generic markdown", "owner/repo", "README.md", "# Project", "configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := DocFromRawContent(tt.repo, tt.path, tt.content)
			if doc.DocType != tt.want {
				t.Errorf("DocType = %q, want %q", doc.DocType, tt.want)
			}
			if doc.Repo != tt.repo {
				t.Errorf("Repo = %q, want %q", doc.Repo, tt.repo)
			}
		})
	}
}

func TestInferDocType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"docs/adr/001.md", "adr"},
		{"ADR-007.md", "adr"},
		{"docs/research/foo.md", "research"},
		{"docs/troubleshooting/bar.md", "troubleshooting"},
		{"docs/plan/roadmap.md", "roadmap"},
		{"src/main.go", "configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := inferDocType(tt.path); got != tt.want {
				t.Errorf("inferDocType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
