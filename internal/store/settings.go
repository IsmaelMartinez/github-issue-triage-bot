package store

import (
	"context"
	"time"
)

// IsPaused returns whether the bot is paused for a given repo.
func (s *Store) IsPaused(ctx context.Context, repo string) (bool, error) {
	var paused bool
	err := s.pool.QueryRow(ctx, `SELECT paused FROM bot_settings WHERE repo = $1`, repo).Scan(&paused)
	if err != nil {
		// No row means not paused
		return false, nil
	}
	return paused, nil
}

// SetPaused sets the pause state for a repo.
func (s *Store) SetPaused(ctx context.Context, repo string, paused bool, by string) error {
	var pausedAt *time.Time
	var pausedBy *string
	if paused {
		now := time.Now()
		pausedAt = &now
		pausedBy = &by
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bot_settings (repo, paused, paused_at, paused_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo) DO UPDATE SET paused = $2, paused_at = $3, paused_by = $4`,
		repo, paused, pausedAt, pausedBy)
	return err
}
