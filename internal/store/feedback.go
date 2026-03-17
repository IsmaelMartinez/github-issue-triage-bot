package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// FeedbackStats holds aggregated feedback signal statistics.
type FeedbackStats struct {
	TotalEditSignals int              `json:"total_edit_signals"`
	TotalMentions    int              `json:"total_mentions"`
	FillRate         *float64         `json:"fill_rate"`
	RecentFeedback   []RecentFeedback `json:"recent_feedback"`
}

// RecentFeedback represents a recent feedback signal for the dashboard.
type RecentFeedback struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	SignalType  string `json:"signal_type"`
	CreatedAt   string `json:"created_at"`
}

// RecordFeedbackSignal inserts a feedback signal row.
func (s *Store) RecordFeedbackSignal(ctx context.Context, sig FeedbackSignal) error {
	details, err := json.Marshal(sig.Details)
	if err != nil {
		return fmt.Errorf("marshal feedback details: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO feedback_signals (repo, issue_number, signal_type, details)
		VALUES ($1, $2, $3, $4)
	`, sig.Repo, sig.IssueNumber, sig.SignalType, details)
	if err != nil {
		return fmt.Errorf("insert feedback signal: %w", err)
	}
	return nil
}

// GetFeedbackStats returns aggregated feedback statistics for the dashboard.
func (s *Store) GetFeedbackStats(ctx context.Context, repo string) (*FeedbackStats, error) {
	fs := &FeedbackStats{RecentFeedback: []RecentFeedback{}}

	// Count distinct issues by signal type (multiple edits/mentions per issue are valid
	// events, but metrics should reflect unique issues affected)
	rows, err := s.pool.Query(ctx, `
		SELECT signal_type, COUNT(DISTINCT issue_number) FROM feedback_signals
		WHERE repo = $1 GROUP BY signal_type
	`, repo)
	if err != nil {
		return nil, fmt.Errorf("feedback signal counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var signalType string
		var count int
		if err := rows.Scan(&signalType, &count); err != nil {
			return nil, err
		}
		switch signalType {
		case "issue_edit_filled":
			fs.TotalEditSignals = count
		case "user_mention":
			fs.TotalMentions = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill rate: edit signals / promoted triage sessions (those with a bot_comment)
	var promoted int
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM triage_sessions t
		INNER JOIN bot_comments b ON t.repo = b.repo AND t.issue_number = b.issue_number
		WHERE t.repo = $1
	`, repo).Scan(&promoted)
	if err != nil {
		return nil, fmt.Errorf("promoted count for fill rate: %w", err)
	}
	if promoted > 0 {
		rate := float64(fs.TotalEditSignals) / float64(promoted)
		fs.FillRate = &rate
	}

	// Recent 10 feedback signals
	rows2, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, signal_type, created_at
		FROM feedback_signals WHERE repo = $1
		ORDER BY created_at DESC LIMIT 10
	`, repo)
	if err != nil {
		return nil, fmt.Errorf("recent feedback: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var rf RecentFeedback
		var createdAt time.Time
		if err := rows2.Scan(&rf.Repo, &rf.IssueNumber, &rf.SignalType, &createdAt); err != nil {
			return nil, err
		}
		rf.CreatedAt = createdAt.Format(time.RFC3339)
		fs.RecentFeedback = append(fs.RecentFeedback, rf)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	return fs, nil
}
