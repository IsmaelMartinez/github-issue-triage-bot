package store

import "time"

// Document represents a documentation chunk (troubleshooting, roadmap, ADR, research).
type Document struct {
	ID        int64
	Repo      string
	DocType   string
	Title     string
	Content   string
	Metadata  map[string]any
	Embedding []float32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Issue represents a GitHub issue with its embedding.
type Issue struct {
	ID        int64
	Repo      string
	Number    int
	Title     string
	Summary   string
	State     string
	Labels    []string
	Milestone *string
	Embedding []float32
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  *time.Time
}

// BotComment tracks a bot comment on a GitHub issue.
type BotComment struct {
	ID          int64
	Repo        string
	IssueNumber int
	CommentID   int64
	PhasesRun   []string
	ThumbsUp    int
	ThumbsDown  int
	CreatedAt   time.Time
}

// SimilarDocument is returned from similarity search with a distance score.
type SimilarDocument struct {
	Document
	Distance float64
}

// SimilarIssue is returned from similarity search with a distance score.
type SimilarIssue struct {
	Issue
	Distance float64
}

// EnhancementDocTypes lists the document types that Phase 4a searches for
// enhancement context. The seed command validates against this list.
var EnhancementDocTypes = []string{"roadmap", "adr", "research"}
