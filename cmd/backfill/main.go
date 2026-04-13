package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	commentpkg "github.com/IsmaelMartinez/github-issue-triage-bot/internal/comment"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL is required\n")
		os.Exit(1)
	}
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		fmt.Fprintf(os.Stderr, "GEMINI_API_KEY is required\n")
		os.Exit(1)
	}
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		fmt.Fprintf(os.Stderr, "GITHUB_TOKEN is required\n")
		os.Exit(1)
	}

	repo := os.Getenv("REPO")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}
	dataRepo := os.Getenv("SOURCE_REPO")
	if dataRepo == "" {
		dataRepo = repo
	}

	limit := 50
	if l := os.Getenv("BACKFILL_LIMIT"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	ctx := context.Background()

	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)
	llmClient := llm.New(geminiAPIKey, logger)
	httpClient := &http.Client{Timeout: 30 * time.Second}

	logger.Info("fetching closed issues", "repo", repo, "limit", limit)
	issues, err := fetchClosedIssues(ctx, httpClient, githubToken, repo, limit)
	if err != nil {
		logger.Error("failed to fetch issues", "error", err)
		os.Exit(1)
	}
	logger.Info("fetched issues", "count", len(issues))

	for i, iss := range issues {
		issLog := logger.With("issue", iss.Number, "index", i)

		isBug := hasLabel(iss.Labels, "bug")
		isEnhancement := hasLabel(iss.Labels, "enhancement")

		var result commentpkg.TriageResult
		result.IsBug = isBug
		result.IsEnhancement = isEnhancement

		result.Phase1 = phases.Phase1(iss.Body)

		queryText := fmt.Sprintf("%s\n%s", phases.Truncate(iss.Title, 200), phases.StripCodeFences(iss.Body, 1500))
		preEmbedding, embedErr := llmClient.Embed(ctx, queryText)
		if embedErr != nil {
			issLog.Warn("shared embed failed, falling back to per-phase embedding", "error", embedErr)
			preEmbedding = nil
		}

		p2, err := phases.Phase2(ctx, s, llmClient, issLog, dataRepo, iss.Title, iss.Body, "", preEmbedding, "")
		if err != nil {
			issLog.Error("phase 2 failed", "error", err)
		}
		result.Phase2 = p2

		p4a, err := phases.Phase4a(ctx, s, llmClient, issLog, dataRepo, iss.Title, iss.Body, preEmbedding, "")
		if err != nil {
			issLog.Error("phase 4a failed", "error", err)
		}
		result.Phase4a = p4a

		body := commentpkg.Build(result)
		phasesRun := collectPhasesRun(result)

		if body == "" {
			issLog.Info("no triage output", "phases", phasesRun)
			continue
		}

		if err := s.RecordBotComment(ctx, store.BotComment{
			Repo:        repo,
			IssueNumber: iss.Number,
			CommentID:   0, // not actually posted
			PhasesRun:   phasesRun,
		}); err != nil {
			issLog.Error("recording backfill result", "error", err)
		} else {
			issLog.Info("backfill complete", "phases", phasesRun, "bodyLen", len(body))
		}

		// Rate limit: 1 second between issues to stay under Gemini free tier
		time.Sleep(1 * time.Second)
	}

	logger.Info("backfill finished", "processed", len(issues))
}

type ghIssue struct {
	Number int       `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
}

type ghLabel struct {
	Name string `json:"name"`
}

func fetchClosedIssues(ctx context.Context, client *http.Client, token, repo string, limit int) ([]ghIssue, error) {
	var all []ghIssue
	page := 1
	perPage := 100
	if limit < perPage {
		perPage = limit
	}

	for len(all) < limit {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=closed&sort=updated&direction=desc&per_page=%d&page=%d", repo, perPage, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
		}

		var issues []ghIssue
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		// GitHub /issues endpoint includes PRs; filter them out (PRs have pull_request key)
		for _, iss := range issues {
			if len(all) >= limit {
				break
			}
			all = append(all, iss)
		}

		if len(issues) < perPage {
			break
		}
		page++
		time.Sleep(500 * time.Millisecond)
	}

	return all, nil
}

func hasLabel(labels []ghLabel, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

func collectPhasesRun(r commentpkg.TriageResult) []string {
	var p []string
	p = append(p, "missing_info")
	if r.Phase2 != nil {
		p = append(p, "doc_search")
	}
	if r.Phase4a != nil {
		p = append(p, "enhancement_context")
	}
	return p
}
