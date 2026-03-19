package synthesis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Runner orchestrates synthesizers and posts briefings to a shadow repo.
type Runner struct {
	synthesizers []Synthesizer
	github       *gh.Client
	store        *store.Store
	logger       *slog.Logger
}

// NewRunner creates a runner with the given synthesizers.
func NewRunner(github *gh.Client, s *store.Store, logger *slog.Logger, synthesizers ...Synthesizer) *Runner {
	return &Runner{synthesizers: synthesizers, github: github, store: s, logger: logger}
}

// Run executes all synthesizers and posts a combined briefing as a shadow issue.
// Returns the number of findings and any error from posting.
func (r *Runner) Run(ctx context.Context, installationID int64, repo, shadowRepo string, window time.Duration) (int, error) {
	var allFindings []Finding

	for _, s := range r.synthesizers {
		findings, err := s.Analyze(ctx, repo, window)
		if err != nil {
			r.logger.Error("synthesizer failed", "name", s.Name(), "error", err)
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	date := time.Now().Format("2006-01-02")
	briefing := BuildBriefing(date, allFindings)
	title := fmt.Sprintf("[Briefing] Weekly — %s", date)

	issueNumber, err := r.github.CreateIssue(ctx, installationID, shadowRepo, title, briefing)
	if err != nil {
		return len(allFindings), fmt.Errorf("posting briefing: %w", err)
	}

	// Record briefing event in journal (best-effort)
	if r.store != nil {
		if evErr := r.store.RecordEvent(ctx, store.RepoEvent{
			Repo:      repo,
			EventType: "briefing_posted",
			SourceRef: fmt.Sprintf("#%d", issueNumber),
			Summary:   title,
			Metadata:  map[string]any{"findings": len(allFindings)},
		}); evErr != nil {
			r.logger.Error("recording briefing event", "error", evErr)
		}
	}

	r.logger.Info("briefing posted", "repo", repo, "findings", len(allFindings))
	return len(allFindings), nil
}
