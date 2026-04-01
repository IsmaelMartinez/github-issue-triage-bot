package mirror

import (
	"testing"
)

func TestValidateRepoFormat(t *testing.T) {
	tests := []struct {
		repo  string
		valid bool
	}{
		{"owner/repo", true},
		{"IsmaelMartinez/teams-for-linux", true},
		{"owner/repo-name.v2", true},
		{"owner/repo_name", true},
		{"", false},
		{"noslash", false},
		{"../../etc/passwd", false},
		{"owner/repo;rm -rf /", false},
		{"owner/repo\ninjection", false},
		{"owner/repo@attacker.com", false},
		{"a/b/c", false},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got := validRepoSlug(tt.repo)
			if got != tt.valid {
				t.Errorf("validRepoSlug(%q) = %v, want %v", tt.repo, got, tt.valid)
			}
		})
	}
}

func TestRedactTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no token",
			input: "fatal: repository not found",
			want:  "fatal: repository not found",
		},
		{
			name:  "single token",
			input: "https://x-access-token:ghs_abc123@github.com/owner/repo.git",
			want:  "https://x-access-token:***@github.com/owner/repo.git",
		},
		{
			name:  "multiple tokens",
			input: "x-access-token:tok1@github.com and x-access-token:tok2@github.com",
			want:  "x-access-token:***@github.com and x-access-token:***@github.com",
		},
		{
			name:  "token at end without @",
			input: "x-access-token:secret",
			want:  "x-access-token:secret",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactTokens(tt.input)
			if got != tt.want {
				t.Errorf("redactTokens(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
