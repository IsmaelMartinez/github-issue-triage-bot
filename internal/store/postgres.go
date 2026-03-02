package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Store provides database operations for the triage bot.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a new Store with the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertDocument inserts or updates a document and its embedding.
func (s *Store) UpsertDocument(ctx context.Context, doc Document) error {
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
func (s *Store) UpsertIssue(ctx context.Context, issue Issue) error {
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
	rows, err := s.pool.Query(ctx, `
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
	return results, rows.Err()
}

// FindSimilarIssues returns the top-k issues closest to the given embedding, excluding the specified issue number.
func (s *Store) FindSimilarIssues(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]SimilarIssue, error) {
	rows, err := s.pool.Query(ctx, `
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

// ConnectPool creates a new pgxpool connection pool from a database URL.
func ConnectPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Register pgvector type
		_, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
		return err
	}
	return pgxpool.NewWithConfig(ctx, config)
}
