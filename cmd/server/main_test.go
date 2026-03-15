package main

import "testing"

func TestParseShadowRepos(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "empty string",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "single mapping",
			input: "owner/repo:owner/shadow",
			want:  map[string]string{"owner/repo": "owner/shadow"},
		},
		{
			name:  "multiple mappings",
			input: "a/b:a/b-shadow,c/d:c/d-shadow",
			want:  map[string]string{"a/b": "a/b-shadow", "c/d": "c/d-shadow"},
		},
		{
			name:  "whitespace trimmed",
			input: " a/b:a/b-shadow , c/d:c/d-shadow ",
			want:  map[string]string{"a/b": "a/b-shadow", "c/d": "c/d-shadow"},
		},
		{
			name:  "invalid entry ignored",
			input: "a/b:a/b-shadow,invalid",
			want:  map[string]string{"a/b": "a/b-shadow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseShadowRepos(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseShadowRepos(%q) returned %d entries, want %d", tt.input, len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseShadowRepos(%q)[%q] = %q, want %q", tt.input, k, got[k], v)
				}
			}
		})
	}
}
