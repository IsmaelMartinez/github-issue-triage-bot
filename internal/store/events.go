package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RepoEvent represents a recorded repository event in the journal.
type RepoEvent struct {
	ID        int64
	Repo      string
	EventType string
	SourceRef string
	Summary   string
	Areas     []string
	Metadata  map[string]any
	CreatedAt time.Time
}

// RecordEvent inserts a single event into the journal.
func (s *Store) RecordEvent(ctx context.Context, event RepoEvent) error {
	meta, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal event metadata: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO repo_events (repo, event_type, source_ref, summary, areas, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.Repo, event.EventType, event.SourceRef, event.Summary, event.Areas, meta)
	if err != nil {
		return fmt.Errorf("insert repo event: %w", err)
	}
	return nil
}

// RecordEvents inserts a batch of events into the journal.
func (s *Store) RecordEvents(ctx context.Context, events []RepoEvent) error {
	for _, e := range events {
		if err := s.RecordEvent(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// ListEvents returns events for a repo within a time window, ordered by created_at DESC.
func (s *Store) ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]RepoEvent, error) {
	var query string
	var args []any

	if len(eventTypes) > 0 {
		query = `
			SELECT id, repo, event_type, source_ref, summary, areas, metadata, created_at
			FROM repo_events
			WHERE repo = $1 AND created_at >= $2 AND event_type = ANY($3)
			ORDER BY created_at DESC LIMIT $4`
		args = []any{repo, since, eventTypes, limit}
	} else {
		query = `
			SELECT id, repo, event_type, source_ref, summary, areas, metadata, created_at
			FROM repo_events
			WHERE repo = $1 AND created_at >= $2
			ORDER BY created_at DESC LIMIT $3`
		args = []any{repo, since, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query repo events: %w", err)
	}
	defer rows.Close()

	var results []RepoEvent
	for rows.Next() {
		var e RepoEvent
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Repo, &e.EventType, &e.SourceRef, &e.Summary, &e.Areas, &meta, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repo event: %w", err)
		}
		_ = json.Unmarshal(meta, &e.Metadata)
		results = append(results, e)
	}
	return results, rows.Err()
}

// CleanupOldEvents deletes events older than the given duration (retention policy).
func (s *Store) CleanupOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `DELETE FROM repo_events WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup old events: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountEvents returns the total number of events for a repo.
func (s *Store) CountEvents(ctx context.Context, repo string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM repo_events WHERE repo = $1`, repo).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count repo events: %w", err)
	}
	return count, nil
}
