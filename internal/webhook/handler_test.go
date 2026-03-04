package webhook

import (
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/comment"
	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
)

func TestSanitizeBody(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		maxLen int
		want   string
	}{
		{
			name:   "short body unchanged",
			body:   "simple text",
			maxLen: 200,
			want:   "simple text",
		},
		{
			name:   "strips code fences",
			body:   "before ```go\nfunc main() {}\n``` after",
			maxLen: 200,
			want:   "before  after",
		},
		{
			name:   "strips unclosed code fence",
			body:   "before ```go\nfunc main() {}",
			maxLen: 200,
			want:   "before",
		},
		{
			name:   "strips HTML tags",
			body:   "hello <b>world</b> end",
			maxLen: 200,
			want:   "hello world end",
		},
		{
			name:   "truncates long body",
			body:   "abcdefghij",
			maxLen: 5,
			want:   "abcde",
		},
		{
			name:   "handles empty body",
			body:   "",
			maxLen: 200,
			want:   "",
		},
		{
			name:   "trims whitespace",
			body:   "  hello  ",
			maxLen: 200,
			want:   "hello",
		},
		{
			name:   "multiple code fences",
			body:   "a ```x``` b ```y``` c",
			maxLen: 200,
			want:   "a  b  c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeBody(tt.body, tt.maxLen)
			if got != tt.want {
				t.Errorf("sanitizeBody(%q, %d) = %q, want %q", tt.body, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestHasLabel(t *testing.T) {
	labels := []gh.LabelInfo{
		{Name: "bug"},
		{Name: "enhancement"},
		{Name: "help wanted"},
	}

	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{"finds existing label", "bug", true},
		{"finds enhancement", "enhancement", true},
		{"finds label with space", "help wanted", true},
		{"returns false for missing", "feature", false},
		{"case sensitive", "Bug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLabel(labels, tt.label); got != tt.want {
				t.Errorf("hasLabel(%v, %q) = %v, want %v", labels, tt.label, got, tt.want)
			}
		})
	}

	t.Run("empty labels", func(t *testing.T) {
		if got := hasLabel(nil, "bug"); got != false {
			t.Errorf("hasLabel(nil, \"bug\") = %v, want false", got)
		}
	})
}

func TestCollectPhasesRun(t *testing.T) {
	tests := []struct {
		name   string
		result comment.TriageResult
		want   []string
	}{
		{
			name:   "phase1 always included",
			result: comment.TriageResult{},
			want:   []string{"phase1"},
		},
		{
			name: "all phases",
			result: comment.TriageResult{
				Phase2:  []phases.Suggestion{{}},
				Phase3:  []phases.Duplicate{{}},
				Phase4a: []phases.ContextMatch{{}},
				Phase4b: &phases.Misclassification{},
			},
			want: []string{"phase1", "phase2", "phase3", "phase4a", "phase4b"},
		},
		{
			name: "bug phases only",
			result: comment.TriageResult{
				IsBug:  true,
				Phase2: []phases.Suggestion{{}},
				Phase3: []phases.Duplicate{{}},
				Phase4b: &phases.Misclassification{},
			},
			want: []string{"phase1", "phase2", "phase3", "phase4b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectPhasesRun(tt.result)
			if len(got) != len(tt.want) {
				t.Fatalf("collectPhasesRun() returned %v, want %v", got, tt.want)
			}
			for i, phase := range got {
				if phase != tt.want[i] {
					t.Errorf("collectPhasesRun()[%d] = %q, want %q", i, phase, tt.want[i])
				}
			}
		})
	}
}
