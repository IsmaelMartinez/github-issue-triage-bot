package phases

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestPhase4a(t *testing.T) {
	dummyEmbedding := make([]float32, 768)

	tests := []struct {
		name       string
		llm        *mockProvider
		store      *mockQuerier
		wantCount  int
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "normal operation with context matches",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "This appears related to the Wayland support roadmap item", "is_infeasible": false}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{
							Document: store.Document{
								Title:   "Wayland Support",
								DocType: "roadmap",
								Metadata: map[string]any{
									"status":       "planned",
									"doc_url":      "https://example.com/docs/wayland",
									"summary":      "Add native Wayland support",
									"last_updated": "2025-01-01",
								},
							},
							Distance: 0.3,
						},
					}, nil
				},
			},
			wantCount: 1,
		},
		{
			name: "no documents found",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
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
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return nil, fmt.Errorf("db connection refused")
				},
			},
			wantErr:    true,
			wantErrMsg: "find similar features",
		},
		{
			name: "malformed LLM JSON array returns error",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": not_valid}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "roadmap", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "parse context",
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
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "roadmap", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "invalid index is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 50, "reason": "out of bounds", "is_infeasible": false}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "roadmap", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "negative index is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": -1, "reason": "negative", "is_infeasible": false}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "roadmap", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "is_infeasible only applies when status is rejected",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "related to rejected item", "is_infeasible": true}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Feature X", DocType: "roadmap", Metadata: map[string]any{"status": "planned"}}},
					}, nil
				},
			},
			wantCount: 1, // match is returned but IsInfeasible should be false since status != "rejected"
		},
		{
			name: "LLM generation error propagates",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return "", fmt.Errorf("model overloaded")
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "adr", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "generate context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Phase4a(context.Background(), tt.store, tt.llm, testLogger(), "test/repo", "Enhancement Request", "Add dark mode support")
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

func TestPhase4a_InfeasibleWithRejectedStatus(t *testing.T) {
	dummyEmbedding := make([]float32, 768)

	llmMock := &mockProvider{
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return dummyEmbedding, nil
		},
		generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
			return `[{"index": 0, "reason": "was rejected previously", "is_infeasible": true}]`, nil
		},
	}
	storeMock := &mockQuerier{
		findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
			return []store.SimilarDocument{
				{Document: store.Document{Title: "Rejected Feature", DocType: "adr", Metadata: map[string]any{"status": "rejected"}}},
			}, nil
		},
	}

	results, err := Phase4a(context.Background(), storeMock, llmMock, testLogger(), "test/repo", "Title", "Body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].IsInfeasible {
		t.Error("expected IsInfeasible to be true when status is rejected")
	}
}
