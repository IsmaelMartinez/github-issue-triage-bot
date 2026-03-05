package store

import (
	"context"
	"time"
)

// DashboardStats holds aggregated statistics for the dashboard.
type DashboardStats struct {
	TotalComments   int             `json:"total_comments"`
	TotalThumbsUp   int             `json:"total_thumbs_up"`
	TotalThumbsDown int             `json:"total_thumbs_down"`
	PhaseBreakdown  map[string]int  `json:"phase_breakdown"`
	DocumentCounts  map[string]int  `json:"document_counts"`
	IssueCount      int             `json:"issue_count"`
	RecentComments  []RecentComment `json:"recent_comments"`
}

// RecentComment represents a recent bot comment for the dashboard.
type RecentComment struct {
	Repo        string   `json:"repo"`
	IssueNumber int      `json:"issue_number"`
	CommentID   int64    `json:"comment_id"`
	PhasesRun   []string `json:"phases_run"`
	ThumbsUp    int      `json:"thumbs_up"`
	ThumbsDown  int      `json:"thumbs_down"`
	CreatedAt   string   `json:"created_at"`
}

// GetDashboardStats retrieves aggregated triage statistics for a given repo.
func (s *Store) GetDashboardStats(ctx context.Context, repo string) (*DashboardStats, error) {
	stats := &DashboardStats{
		PhaseBreakdown: make(map[string]int),
		DocumentCounts: make(map[string]int),
		RecentComments: []RecentComment{},
	}

	// Total comments, sum thumbs_up, sum thumbs_down
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(thumbs_up), 0), COALESCE(SUM(thumbs_down), 0)
		FROM bot_comments WHERE repo = $1
	`, repo).Scan(&stats.TotalComments, &stats.TotalThumbsUp, &stats.TotalThumbsDown)
	if err != nil {
		return nil, err
	}

	// Phase breakdown
	rows, err := s.pool.Query(ctx, `
		SELECT phase, count(*) FROM bot_comments, unnest(phases_run) AS phase
		WHERE repo = $1 GROUP BY phase
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var phase string
		var count int
		if err := rows.Scan(&phase, &count); err != nil {
			return nil, err
		}
		stats.PhaseBreakdown[phase] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Document counts by type
	rows2, err := s.pool.Query(ctx, `
		SELECT doc_type, count(*) FROM documents WHERE repo = $1 GROUP BY doc_type
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var docType string
		var count int
		if err := rows2.Scan(&docType, &count); err != nil {
			return nil, err
		}
		stats.DocumentCounts[docType] = count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Issue count
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM issues WHERE repo = $1
	`, repo).Scan(&stats.IssueCount)
	if err != nil {
		return nil, err
	}

	// Recent 20 comments
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
		ORDER BY created_at DESC LIMIT 20
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var rc RecentComment
		var createdAt time.Time
		if err := rows3.Scan(&rc.Repo, &rc.IssueNumber, &rc.CommentID, &rc.PhasesRun, &rc.ThumbsUp, &rc.ThumbsDown, &createdAt); err != nil {
			return nil, err
		}
		rc.CreatedAt = createdAt.Format(time.RFC3339)
		stats.RecentComments = append(stats.RecentComments, rc)
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

// UpdateReactions updates the thumbs up/down counts for a bot comment.
func (s *Store) UpdateReactions(ctx context.Context, repo string, issueNumber, thumbsUp, thumbsDown int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE bot_comments SET thumbs_up = $3, thumbs_down = $4
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber, thumbsUp, thumbsDown)
	return err
}

// ListBotComments returns all bot comments for a given repo.
func (s *Store) ListBotComments(ctx context.Context, repo string) ([]BotComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []BotComment
	for rows.Next() {
		var bc BotComment
		if err := rows.Scan(&bc.ID, &bc.Repo, &bc.IssueNumber, &bc.CommentID, &bc.PhasesRun, &bc.ThumbsUp, &bc.ThumbsDown, &bc.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, bc)
	}
	return comments, rows.Err()
}
