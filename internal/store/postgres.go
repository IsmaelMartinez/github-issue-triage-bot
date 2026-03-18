package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// PhaseQuerier defines the database operations that triage phases require.
type PhaseQuerier interface {
	FindSimilarDocuments(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]SimilarDocument, error)
	FindSimilarIssues(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]SimilarIssue, error)
}

// Store provides database operations for the triage bot.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a new Store with the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Pool returns the underlying connection pool. Used by integration tests for cleanup.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

const EmbeddingDim = 768

// UpsertDocument inserts or updates a document and its embedding.
func (s *Store) UpsertDocument(ctx context.Context, doc Document) error {
	if len(doc.Embedding) != EmbeddingDim {
		return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(doc.Embedding), EmbeddingDim)
	}
	meta, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO documents (repo, doc_type, title, content, metadata, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo, doc_type, title) DO UPDATE SET
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding,
			updated_at = now()
	`, doc.Repo, doc.DocType, doc.Title, doc.Content, meta, pgvector.NewVector(doc.Embedding))
	return err
}

// UpsertIssue inserts or updates an issue and its embedding.
// If issue.CreatedAt is set (non-zero), it is used as the issue creation timestamp;
// otherwise the database default (now()) applies. On conflict, created_at is never overwritten.
func (s *Store) UpsertIssue(ctx context.Context, issue Issue) error {
	if len(issue.Embedding) != EmbeddingDim {
		return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(issue.Embedding), EmbeddingDim)
	}
	if !issue.CreatedAt.IsZero() {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO issues (repo, number, title, summary, state, labels, milestone, embedding, closed_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (repo, number) DO UPDATE SET
				title = EXCLUDED.title,
				summary = EXCLUDED.summary,
				state = EXCLUDED.state,
				labels = EXCLUDED.labels,
				milestone = EXCLUDED.milestone,
				embedding = EXCLUDED.embedding,
				closed_at = EXCLUDED.closed_at,
				updated_at = now()
		`, issue.Repo, issue.Number, issue.Title, issue.Summary, issue.State,
			issue.Labels, issue.Milestone, pgvector.NewVector(issue.Embedding), issue.ClosedAt, issue.CreatedAt)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO issues (repo, number, title, summary, state, labels, milestone, embedding, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (repo, number) DO UPDATE SET
			title = EXCLUDED.title,
			summary = EXCLUDED.summary,
			state = EXCLUDED.state,
			labels = EXCLUDED.labels,
			milestone = EXCLUDED.milestone,
			embedding = EXCLUDED.embedding,
			closed_at = EXCLUDED.closed_at,
			updated_at = now()
	`, issue.Repo, issue.Number, issue.Title, issue.Summary, issue.State,
		issue.Labels, issue.Milestone, pgvector.NewVector(issue.Embedding), issue.ClosedAt)
	return err
}

// FindSimilarDocuments returns the top-k documents closest to the given embedding, filtered by repo and doc types.
func (s *Store) FindSimilarDocuments(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]SimilarDocument, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.Exec(ctx, "SET LOCAL ivfflat.probes = 5"); err != nil {
		return nil, fmt.Errorf("set ivfflat.probes: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, repo, doc_type, title, content, metadata, embedding,
		       created_at, updated_at,
		       embedding <=> $1 AS distance
		FROM documents
		WHERE repo = $2 AND doc_type = ANY($3)
		ORDER BY embedding <=> $1
		LIMIT $4
	`, pgvector.NewVector(embedding), repo, docTypes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimilarDocument
	for rows.Next() {
		var sd SimilarDocument
		var meta []byte
		var vec pgvector.Vector
		err := rows.Scan(
			&sd.ID, &sd.Repo, &sd.DocType, &sd.Title, &sd.Content, &meta, &vec,
			&sd.CreatedAt, &sd.UpdatedAt,
			&sd.Distance,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &sd.Metadata)
		sd.Embedding = vec.Slice()
		results = append(results, sd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, tx.Commit(ctx)
}

// FindSimilarIssues returns the top-k issues closest to the given embedding, excluding the specified issue number.
func (s *Store) FindSimilarIssues(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]SimilarIssue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.Exec(ctx, "SET LOCAL ivfflat.probes = 6"); err != nil {
		return nil, fmt.Errorf("set ivfflat.probes: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, repo, number, title, summary, state, labels, milestone,
		       embedding, created_at, updated_at, closed_at,
		       embedding <=> $1 AS distance
		FROM issues
		WHERE repo = $2 AND number != $3
		ORDER BY embedding <=> $1
		LIMIT $4
	`, pgvector.NewVector(embedding), repo, excludeNumber, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimilarIssue
	for rows.Next() {
		var si SimilarIssue
		var vec pgvector.Vector
		err := rows.Scan(
			&si.ID, &si.Repo, &si.Number, &si.Title, &si.Summary, &si.State,
			&si.Labels, &si.Milestone, &vec, &si.CreatedAt, &si.UpdatedAt, &si.ClosedAt,
			&si.Distance,
		)
		if err != nil {
			return nil, err
		}
		si.Embedding = vec.Slice()
		results = append(results, si)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, tx.Commit(ctx)
}

// RecentIssuesWithEmbeddings returns issues opened within the time window that have embeddings.
func (s *Store) RecentIssuesWithEmbeddings(ctx context.Context, repo string, since time.Time) ([]Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, number, title, summary, state, labels,
		       embedding, created_at, updated_at
		FROM issues
		WHERE repo = $1 AND created_at >= $2 AND embedding IS NOT NULL
		ORDER BY created_at DESC
	`, repo, since)
	if err != nil {
		return nil, fmt.Errorf("query recent issues: %w", err)
	}
	defer rows.Close()

	var results []Issue
	for rows.Next() {
		var i Issue
		var vec pgvector.Vector
		if err := rows.Scan(&i.ID, &i.Repo, &i.Number, &i.Title, &i.Summary,
			&i.State, &i.Labels, &vec, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan recent issue: %w", err)
		}
		i.Embedding = vec.Slice()
		results = append(results, i)
	}
	return results, rows.Err()
}

// ListDocumentsByType returns all documents of the given types for a repo.
func (s *Store) ListDocumentsByType(ctx context.Context, repo string, docTypes []string) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, doc_type, title, content, metadata, created_at, updated_at
		FROM documents
		WHERE repo = $1 AND doc_type = ANY($2)
		ORDER BY updated_at DESC
	`, repo, docTypes)
	if err != nil {
		return nil, fmt.Errorf("query documents by type: %w", err)
	}
	defer rows.Close()

	var results []Document
	for rows.Next() {
		var d Document
		var meta []byte
		if err := rows.Scan(&d.ID, &d.Repo, &d.DocType, &d.Title, &d.Content,
			&meta, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		_ = json.Unmarshal(meta, &d.Metadata)
		results = append(results, d)
	}
	return results, rows.Err()
}

// RecentDocumentsByType returns documents of the given types ingested since the given time.
func (s *Store) RecentDocumentsByType(ctx context.Context, repo string, docTypes []string, since time.Time) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, doc_type, title, content, metadata, embedding, created_at, updated_at
		FROM documents
		WHERE repo = $1 AND doc_type = ANY($2) AND updated_at >= $3
		ORDER BY updated_at DESC
	`, repo, docTypes, since)
	if err != nil {
		return nil, fmt.Errorf("query recent documents: %w", err)
	}
	defer rows.Close()

	var results []Document
	for rows.Next() {
		var d Document
		var meta []byte
		var vec pgvector.Vector
		if err := rows.Scan(&d.ID, &d.Repo, &d.DocType, &d.Title, &d.Content,
			&meta, &vec, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan recent document: %w", err)
		}
		_ = json.Unmarshal(meta, &d.Metadata)
		d.Embedding = vec.Slice()
		results = append(results, d)
	}
	return results, rows.Err()
}

// HasBotCommented checks if the bot has already commented on the given issue.
func (s *Store) HasBotCommented(ctx context.Context, repo string, issueNumber int) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM bot_comments WHERE repo = $1 AND issue_number = $2)
	`, repo, issueNumber).Scan(&exists)
	return exists, err
}

// RecordBotComment records that the bot commented on an issue.
func (s *Store) RecordBotComment(ctx context.Context, comment BotComment) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bot_comments (repo, issue_number, comment_id, phases_run)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo, issue_number) DO UPDATE SET
			comment_id = EXCLUDED.comment_id,
			phases_run = EXCLUDED.phases_run
	`, comment.Repo, comment.IssueNumber, comment.CommentID, comment.PhasesRun)
	return err
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// CheckAndRecordDelivery atomically checks if a webhook delivery ID has been seen before
// and records it if not. Returns true if the delivery was already recorded (duplicate).
func (s *Store) CheckAndRecordDelivery(ctx context.Context, deliveryID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO webhook_deliveries (delivery_id) VALUES ($1)
			ON CONFLICT (delivery_id) DO NOTHING
			RETURNING delivery_id
		)
		SELECT NOT EXISTS(SELECT 1 FROM ins)
	`, deliveryID).Scan(&exists)
	return exists, err
}

// CleanupOldDeliveries deletes webhook delivery records older than the given duration.
// Returns the number of rows deleted.
func (s *Store) CleanupOldDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhook_deliveries WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ConnectPool creates a new pgxpool connection pool from a database URL.
func ConnectPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	config.AfterConnect = pgxvector.RegisterTypes
	return pgxpool.NewWithConfig(ctx, config)
}
