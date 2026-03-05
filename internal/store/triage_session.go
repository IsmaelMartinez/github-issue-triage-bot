package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// CreateTriageSession inserts a new triage session record.
func (s *Store) CreateTriageSession(ctx context.Context, ts TriageSession) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO triage_sessions (repo, issue_number, shadow_repo, shadow_issue_number, triage_comment, phases_run)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo, issue_number) DO UPDATE
			SET shadow_repo = EXCLUDED.shadow_repo,
				shadow_issue_number = EXCLUDED.shadow_issue_number,
				triage_comment = EXCLUDED.triage_comment,
				phases_run = EXCLUDED.phases_run
	`, ts.Repo, ts.IssueNumber, ts.ShadowRepo, ts.ShadowIssueNumber, ts.TriageComment, ts.PhasesRun)
	return err
}

// GetTriageSessionByShadow returns the triage session for a given shadow issue, or nil if not found.
func (s *Store) GetTriageSessionByShadow(ctx context.Context, shadowRepo string, shadowIssueNumber int) (*TriageSession, error) {
	var ts TriageSession
	err := s.pool.QueryRow(ctx, `
		SELECT id, repo, issue_number, shadow_repo, shadow_issue_number, triage_comment, phases_run, created_at
		FROM triage_sessions WHERE shadow_repo = $1 AND shadow_issue_number = $2
	`, shadowRepo, shadowIssueNumber).Scan(
		&ts.ID, &ts.Repo, &ts.IssueNumber, &ts.ShadowRepo, &ts.ShadowIssueNumber, &ts.TriageComment, &ts.PhasesRun, &ts.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &ts, nil
}

// HasTriageSession returns true if a triage session already exists for the given issue.
func (s *Store) HasTriageSession(ctx context.Context, repo string, issueNumber int) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM triage_sessions WHERE repo = $1 AND issue_number = $2)
	`, repo, issueNumber).Scan(&exists)
	return exists, err
}
