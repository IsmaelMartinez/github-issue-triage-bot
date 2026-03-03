package phases

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestPhase3(t *testing.T) {
	dummyEmbedding := make([]float32, 768)
	closedAt := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		llm        *mockProvider
		store      *mockQuerier
		wantCount  int
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "normal operation with duplicates",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"number": 42, "reason": "This might be related because both describe login crashes", "similarity": 85}]`, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{
							Issue: store.Issue{
								Number:   42,
								Title:    "Login crash on startup",
								State:    "closed",
								ClosedAt: &closedAt,
							},
							Distance: 0.15,
						},
					}, nil
				},
			},
			wantCount: 1,
		},
		{
			name: "no similar issues found",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return nil, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "embed error propagates",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return nil, fmt.Errorf("embed failed")
				},
			},
			store:      &mockQuerier{},
			wantErr:    true,
			wantErrMsg: "embed issue",
		},
		{
			name: "store error propagates",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return nil, fmt.Errorf("db error")
				},
			},
			wantErr:    true,
			wantErrMsg: "find similar issues",
		},
		{
			name: "malformed LLM JSON array returns error",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"number": invalid}]`, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 1, Title: "Issue"}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "parse duplicates",
		},
		{
			name: "LLM response with no JSON brackets returns empty results",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return "no json here", nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 1, Title: "Issue"}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "issue number not in search results is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"number": 999, "reason": "not in results", "similarity": 80}]`, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 42, Title: "Real issue"}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "similarity below 60 is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"number": 42, "reason": "low similarity", "similarity": 50}]`, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 42, Title: "Some issue"}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "similarity above 100 is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"number": 42, "reason": "impossible similarity", "similarity": 101}]`, nil
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 42, Title: "Some issue"}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "LLM generation error propagates",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return "", fmt.Errorf("api timeout")
				},
			},
			store: &mockQuerier{
				findIssuesFunc: func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
					return []store.SimilarIssue{
						{Issue: store.Issue{Number: 42, Title: "Issue"}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "generate duplicates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Phase3(context.Background(), tt.store, tt.llm, testLogger(), "test/repo", 1, "New Issue", "Issue body")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("got %d results, want %d", len(results), tt.wantCount)
			}
		})
	}
}
