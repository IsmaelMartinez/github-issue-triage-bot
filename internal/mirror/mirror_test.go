package mirror

import (
	"testing"
)

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
