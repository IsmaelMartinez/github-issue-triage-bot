package store

import (
	"context"
	"fmt"
	"regexp"
)

// DocReference represents a cross-reference between documents, issues, or PRs.
type DocReference struct {
	ID           int64
	Repo         string
	SourceType   string
	SourceID     string
	TargetType   string
	TargetID     string
	Relationship string
}

var (
	issueRefRe = regexp.MustCompile(`#(\d+)`)
	adrRefRe   = regexp.MustCompile(`ADR-(\d+)`)
)

// ExtractReferences finds issue and ADR references in text content using regex.
func ExtractReferences(content string) []DocReference {
	seen := make(map[string]bool)
	var refs []DocReference

	for _, match := range issueRefRe.FindAllString(content, -1) {
		if !seen[match] {
			seen[match] = true
			refs = append(refs, DocReference{
				TargetType:   "issue",
				TargetID:     match,
				Relationship: "references",
			})
		}
	}

	for _, match := range adrRefRe.FindAllString(content, -1) {
		if !seen[match] {
			seen[match] = true
			refs = append(refs, DocReference{
				TargetType:   "document",
				TargetID:     match,
				Relationship: "references",
			})
		}
	}

	return refs
}

// RecordReferences inserts cross-references for a source document or issue.
func (s *Store) RecordReferences(ctx context.Context, repo, sourceType, sourceID string, refs []DocReference) error {
	for _, ref := range refs {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO doc_references (repo, source_type, source_id, target_type, target_id, relationship)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (repo, source_type, source_id, target_type, target_id, relationship) DO NOTHING
		`, repo, sourceType, sourceID, ref.TargetType, ref.TargetID, ref.Relationship)
		if err != nil {
			return fmt.Errorf("insert doc reference: %w", err)
		}
	}
	return nil
}

// FindReferencesTo returns all documents/issues that reference the given target.
func (s *Store) FindReferencesTo(ctx context.Context, repo, targetType, targetID string) ([]DocReference, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, source_type, source_id, target_type, target_id, relationship
		FROM doc_references
		WHERE repo = $1 AND target_type = $2 AND target_id = $3
	`, repo, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("query references to: %w", err)
	}
	defer rows.Close()

	var results []DocReference
	for rows.Next() {
		var r DocReference
		if err := rows.Scan(&r.ID, &r.Repo, &r.SourceType, &r.SourceID, &r.TargetType, &r.TargetID, &r.Relationship); err != nil {
			return nil, fmt.Errorf("scan doc reference: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// CountReferencesTo returns the count of references to a given target.
func (s *Store) CountReferencesTo(ctx context.Context, repo, targetType, targetID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM doc_references
		WHERE repo = $1 AND target_type = $2 AND target_id = $3
	`, repo, targetType, targetID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count references to: %w", err)
	}
	return count, nil
}
