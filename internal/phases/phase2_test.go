package phases

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// testLogger returns a no-op logger for use in tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	embedFunc               func(ctx context.Context, text string) ([]float32, error)
	generateJSONFunc        func(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error)
	generateJSONWithSysFunc func(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error)
}

func (m *mockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return m.embedFunc(ctx, text)
}

func (m *mockProvider) GenerateJSON(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, error) {
	return m.generateJSONFunc(ctx, prompt, temperature, maxTokens)
}

func (m *mockProvider) GenerateJSONWithSystem(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error) {
	return m.generateJSONWithSysFunc(ctx, systemPrompt, userContent, temperature, maxTokens)
}

// mockQuerier implements store.PhaseQuerier for testing.
type mockQuerier struct {
	findDocsFunc   func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error)
	findIssuesFunc func(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error)
}

func (m *mockQuerier) FindSimilarDocuments(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
	return m.findDocsFunc(ctx, repo, docTypes, embedding, limit)
}

func (m *mockQuerier) FindSimilarIssues(ctx context.Context, repo string, embedding []float32, excludeNumber int, limit int) ([]store.SimilarIssue, error) {
	return m.findIssuesFunc(ctx, repo, embedding, excludeNumber, limit)
}

func TestPhase2(t *testing.T) {
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
			name: "normal operation with matches",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "This appears similar because of login issues. Try clearing the cache.", "relevance": 75}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{
							Document: store.Document{
								Title:   "Login Troubleshooting",
								Content: "Clear cache to fix login",
								Metadata: map[string]any{
									"category":    "auth",
									"description": "Issues with login flow",
									"docUrl":      "https://example.com/docs/login",
								},
							},
							Distance: 0.2,
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
					return nil, fmt.Errorf("db connection failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "find similar docs",
		},
		{
			name: "malformed LLM JSON array returns error",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": not_a_number}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "parse suggestions",
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
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "invalid index in LLM response is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 99, "reason": "out of bounds", "relevance": 75}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
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
					return "", fmt.Errorf("rate limit exceeded")
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "generate suggestions",
		},
		{
			name: "empty reason is skipped",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "", "relevance": 75}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "low relevance is filtered",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "weak connection", "relevance": 40}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "troubleshooting doc at 65% filtered by higher category threshold",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "weak troubleshooting match", "relevance": 65}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Doc", DocType: "troubleshooting", Metadata: map[string]any{}}},
					}, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "configuration doc at 55% passes lower category threshold",
			llm: &mockProvider{
				embedFunc: func(ctx context.Context, text string) ([]float32, error) {
					return dummyEmbedding, nil
				},
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `[{"index": 0, "reason": "related config option", "relevance": 55}]`, nil
				},
			},
			store: &mockQuerier{
				findDocsFunc: func(ctx context.Context, repo string, docTypes []string, embedding []float32, limit int) ([]store.SimilarDocument, error) {
					return []store.SimilarDocument{
						{Document: store.Document{Title: "Config", DocType: "configuration", Metadata: map[string]any{"docUrl": "https://example.com/config"}}},
					}, nil
				},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Phase2(context.Background(), tt.store, tt.llm, testLogger(), "test/repo", "Test Issue", "Test body content", "", nil)
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
