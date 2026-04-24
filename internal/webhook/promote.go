package webhook

import (
	"context"
	"fmt"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// PromoteTriageSession posts the shadow triage comment on the source repo and
// closes the shadow issue. If a bot_comments row already exists for the source
// issue — which happens when a prior attempt succeeded but crashed before
// clearing the pending-promotion marker — the GitHub post is skipped so the
// maintainer doesn't see a duplicate. The caller is responsible for any
// pending_promotion_at or closed_at bookkeeping; this function only handles
// the posting sequence so both the webhook `lgtm` path and the /cleanup retry
// pass can share it.
func PromoteTriageSession(ctx context.Context, gh *gh.Client, s *store.Store, installationID int64, ts store.TriageSession) error {
	already, err := s.HasBotCommented(ctx, ts.Repo, ts.IssueNumber)
	if err != nil {
		return fmt.Errorf("check existing bot comment: %w", err)
	}
	if !already {
		commentID, err := gh.CreateComment(ctx, installationID, ts.Repo, ts.IssueNumber, ts.TriageComment)
		if err != nil {
			return fmt.Errorf("post triage comment publicly: %w", err)
		}
		// Best-effort: the comment is already public; bookkeeping failure here
		// is recoverable by the next dashboard-stats refresh.
		_ = s.RecordBotComment(ctx, store.BotComment{
			Repo:        ts.Repo,
			IssueNumber: ts.IssueNumber,
			CommentID:   commentID,
			PhasesRun:   ts.PhasesRun,
		})
	}
	_ = gh.CloseIssue(ctx, installationID, ts.ShadowRepo, ts.ShadowIssueNumber)
	return nil
}
