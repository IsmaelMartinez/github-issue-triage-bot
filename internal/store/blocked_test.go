package store

import (
	"context"
	"testing"
)

// TestStore_FindSimilarBlockedIssues_CompilesAgainstContract is a compile-only
// test that confirms FindSimilarBlockedIssues exists with the expected signature.
// Real query execution is covered by integration tests.
func TestStore_FindSimilarBlockedIssues_CompilesAgainstContract(t *testing.T) {
	var _ func(ctx context.Context, repo string, embedding []float32, limit int) ([]SimilarIssue, error) = (*Store)(nil).FindSimilarBlockedIssues
}
