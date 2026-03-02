package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: seed <type> <file>\n")
		fmt.Fprintf(os.Stderr, "  type: troubleshooting | issues | features\n")
		os.Exit(1)
	}

	seedType := os.Args[1]
	filePath := os.Args[2]

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL environment variable is required\n")
		os.Exit(1)
	}
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		fmt.Fprintf(os.Stderr, "GEMINI_API_KEY environment variable is required\n")
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)
	l := llm.New(geminiAPIKey)

	data, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error("failed to read file", "error", err)
		os.Exit(1)
	}

	repo := os.Getenv("REPO")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}

	switch seedType {
	case "troubleshooting":
		err = seedTroubleshooting(ctx, s, l, repo, data, logger)
	case "issues":
		err = seedIssues(ctx, s, l, repo, data, logger)
	case "features":
		err = seedFeatures(ctx, s, l, repo, data, logger)
	default:
		fmt.Fprintf(os.Stderr, "unknown seed type: %s\n", seedType)
		os.Exit(1)
	}

	if err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
	logger.Info("seed completed")
}

func seedTroubleshooting(ctx context.Context, s *store.Store, l *llm.Client, repo string, data []byte, logger *slog.Logger) error {
	var entries []struct {
		Title       string   `json:"title"`
		Category    string   `json:"category"`
		Description string   `json:"description"`
		Solutions   string   `json:"solutions"`
		Anchor      string   `json:"anchor"`
		DocURL      string   `json:"docUrl"`
		Related     []string `json:"relatedIssues"`
		Source      string   `json:"source"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse troubleshooting index: %w", err)
	}

	for i, e := range entries {
		text := fmt.Sprintf("%s\n%s\n%s", e.Title, e.Description, e.Solutions)
		embedding, err := l.Embed(ctx, text)
		if err != nil {
			return fmt.Errorf("embed entry %d: %w", i, err)
		}

		doc := store.Document{
			Repo:    repo,
			DocType: e.Source,
			Title:   e.Title,
			Content: e.Solutions,
			Metadata: map[string]any{
				"category":      e.Category,
				"description":   e.Description,
				"anchor":        e.Anchor,
				"docUrl":        e.DocURL,
				"relatedIssues": e.Related,
			},
			Embedding: embedding,
		}
		if err := s.UpsertDocument(ctx, doc); err != nil {
			return fmt.Errorf("upsert entry %d: %w", i, err)
		}
		logger.Info("seeded troubleshooting", "title", e.Title, "index", i)
	}
	return nil
}

func seedIssues(ctx context.Context, s *store.Store, l *llm.Client, repo string, data []byte, logger *slog.Logger) error {
	var entries []struct {
		Number    int      `json:"number"`
		Title     string   `json:"title"`
		State     string   `json:"state"`
		Labels    []string `json:"labels"`
		Summary   string   `json:"summary"`
		CreatedAt string   `json:"created_at"`
		ClosedAt  *string  `json:"closed_at"`
		Milestone *string  `json:"milestone"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse issue index: %w", err)
	}

	for i, e := range entries {
		text := fmt.Sprintf("%s\n%s", e.Title, e.Summary)
		embedding, err := l.Embed(ctx, text)
		if err != nil {
			return fmt.Errorf("embed issue %d: %w", i, err)
		}

		issue := store.Issue{
			Repo:      repo,
			Number:    e.Number,
			Title:     e.Title,
			Summary:   e.Summary,
			State:     e.State,
			Labels:    e.Labels,
			Milestone: e.Milestone,
			Embedding: embedding,
		}
		if err := s.UpsertIssue(ctx, issue); err != nil {
			return fmt.Errorf("upsert issue %d: %w", i, err)
		}
		logger.Info("seeded issue", "number", e.Number, "index", i)
	}
	return nil
}

func seedFeatures(ctx context.Context, s *store.Store, l *llm.Client, repo string, data []byte, logger *slog.Logger) error {
	var entries []struct {
		Topic       string `json:"topic"`
		Status      string `json:"status"`
		DocPath     string `json:"doc_path"`
		DocURL      string `json:"doc_url"`
		Summary     string `json:"summary"`
		Source      string `json:"source"`
		LastUpdated string `json:"last_updated"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse feature index: %w", err)
	}

	for i, e := range entries {
		text := fmt.Sprintf("%s\n%s", e.Topic, e.Summary)
		embedding, err := l.Embed(ctx, text)
		if err != nil {
			return fmt.Errorf("embed feature %d: %w", i, err)
		}

		doc := store.Document{
			Repo:    repo,
			DocType: e.Source,
			Title:   e.Topic,
			Content: e.Summary,
			Metadata: map[string]any{
				"status":       e.Status,
				"doc_path":     e.DocPath,
				"doc_url":      e.DocURL,
				"summary":      e.Summary,
				"last_updated": e.LastUpdated,
			},
			Embedding: embedding,
		}
		if err := s.UpsertDocument(ctx, doc); err != nil {
			return fmt.Errorf("upsert feature %d: %w", i, err)
		}
		logger.Info("seeded feature", "topic", e.Topic, "index", i)
	}
	return nil
}
