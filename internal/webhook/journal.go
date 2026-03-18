package webhook

import (
	"context"
	"fmt"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func issueToRepoEvent(repo, action string, issue gh.IssueDetail) store.RepoEvent {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	return store.RepoEvent{
		Repo:      repo,
		EventType: "issue_" + action,
		SourceRef: fmt.Sprintf("#%d", issue.Number),
		Summary:   issue.Title,
		Metadata:  map[string]any{"labels": labels},
	}
}

func commentToRepoEvent(repo string, issueNumber int, user, body string) store.RepoEvent {
	summary := body
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return store.RepoEvent{
		Repo:      repo,
		EventType: "comment",
		SourceRef: fmt.Sprintf("#%d", issueNumber),
		Summary:   summary,
		Metadata:  map[string]any{"user": user},
	}
}

func pushToRepoEvent(repo, ref string) store.RepoEvent {
	return store.RepoEvent{
		Repo:      repo,
		EventType: "push",
		SourceRef: ref,
		Summary:   fmt.Sprintf("Push to %s", ref),
	}
}

func (h *Handler) recordEvent(ctx context.Context, event store.RepoEvent) {
	if err := h.store.RecordEvent(ctx, event); err != nil {
		h.logger.Error("recording event journal entry", "error", err, "eventType", event.EventType)
	}
}
