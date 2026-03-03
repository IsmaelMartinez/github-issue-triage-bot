package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL environment variable is required\n")
		os.Exit(1)
	}
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		fmt.Fprintf(os.Stderr, "GITHUB_TOKEN environment variable is required\n")
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

	repo := os.Getenv("REPO")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}

	comments, err := s.ListBotComments(ctx, repo)
	if err != nil {
		logger.Error("failed to list bot comments", "error", err)
		os.Exit(1)
	}

	logger.Info("syncing reactions", "comments", len(comments))

	client := &http.Client{Timeout: 15 * time.Second}

	for _, bc := range comments {
		thumbsUp, thumbsDown, err := fetchReactions(ctx, client, githubToken, bc.Repo, bc.CommentID)
		if err != nil {
			logger.Warn("failed to fetch reactions", "comment_id", bc.CommentID, "issue", bc.IssueNumber, "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if err := s.UpdateReactions(ctx, bc.Repo, bc.IssueNumber, thumbsUp, thumbsDown); err != nil {
			logger.Warn("failed to update reactions", "issue", bc.IssueNumber, "error", err)
		} else {
			logger.Info("synced reactions", "issue", bc.IssueNumber, "thumbs_up", thumbsUp, "thumbs_down", thumbsDown)
		}

		time.Sleep(500 * time.Millisecond)
	}

	logger.Info("reaction sync completed")
}

type reaction struct {
	Content string `json:"content"`
}

func fetchReactions(ctx context.Context, client *http.Client, token, repo string, commentID int64) (thumbsUp, thumbsDown int, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d/reactions", repo, commentID)

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, 0, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			return 0, 0, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return 0, 0, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
		}

		var reactions []reaction
		if err := json.NewDecoder(resp.Body).Decode(&reactions); err != nil {
			resp.Body.Close()
			return 0, 0, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		for _, r := range reactions {
			switch r.Content {
			case "+1":
				thumbsUp++
			case "-1":
				thumbsDown++
			}
		}

		// Check for pagination
		nextURL := getNextLink(resp.Header.Get("Link"))
		if nextURL == "" {
			break
		}
		url = nextURL
	}

	return thumbsUp, thumbsDown, nil
}

// getNextLink parses the GitHub Link header for the next page URL.
func getNextLink(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, `rel="next"`) {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start >= 0 && end > start {
				return part[start+1 : end]
			}
		}
	}
	return ""
}
