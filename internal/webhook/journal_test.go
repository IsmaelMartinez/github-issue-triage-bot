package webhook

import (
	"testing"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestIssueEventToRepoEvent(t *testing.T) {
	tests := []struct {
		name   string
		action string
		issue  gh.IssueDetail
		want   store.RepoEvent
	}{
		{
			name:   "opened bug",
			action: "opened",
			issue:  gh.IssueDetail{Number: 42, Title: "Audio broken", Labels: []gh.LabelInfo{{Name: "bug"}}},
			want: store.RepoEvent{
				EventType: "issue_opened",
				SourceRef: "#42",
				Summary:   "Audio broken",
			},
		},
		{
			name:   "closed issue",
			action: "closed",
			issue:  gh.IssueDetail{Number: 10, Title: "Old bug"},
			want: store.RepoEvent{
				EventType: "issue_closed",
				SourceRef: "#10",
				Summary:   "Old bug",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueToRepoEvent("owner/repo", tt.action, tt.issue)
			if got.EventType != tt.want.EventType {
				t.Errorf("EventType = %q, want %q", got.EventType, tt.want.EventType)
			}
			if got.SourceRef != tt.want.SourceRef {
				t.Errorf("SourceRef = %q, want %q", got.SourceRef, tt.want.SourceRef)
			}
			if got.Summary != tt.want.Summary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.want.Summary)
			}
		})
	}
}

func TestCommentToRepoEvent(t *testing.T) {
	t.Run("short body", func(t *testing.T) {
		got := commentToRepoEvent("owner/repo", 5, "alice", "nice fix")
		if got.EventType != "comment" {
			t.Errorf("EventType = %q, want %q", got.EventType, "comment")
		}
		if got.SourceRef != "#5" {
			t.Errorf("SourceRef = %q, want %q", got.SourceRef, "#5")
		}
		if got.Summary != "nice fix" {
			t.Errorf("Summary = %q, want %q", got.Summary, "nice fix")
		}
	})

	t.Run("long body truncated", func(t *testing.T) {
		long := make([]byte, 300)
		for i := range long {
			long[i] = 'x'
		}
		got := commentToRepoEvent("owner/repo", 5, "alice", string(long))
		if len(got.Summary) != 200 {
			t.Errorf("Summary length = %d, want 200", len(got.Summary))
		}
	})
}

func TestPushToRepoEvent(t *testing.T) {
	got := pushToRepoEvent("owner/repo", "refs/heads/main")
	if got.EventType != "push" {
		t.Errorf("EventType = %q, want %q", got.EventType, "push")
	}
	if got.SourceRef != "refs/heads/main" {
		t.Errorf("SourceRef = %q, want %q", got.SourceRef, "refs/heads/main")
	}
	if got.Summary != "Push to refs/heads/main" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Push to refs/heads/main")
	}
}
