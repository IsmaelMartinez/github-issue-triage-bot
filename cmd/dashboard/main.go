package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// RepoNav holds navigation tab data for the template.
type RepoNav struct {
	Label  string
	Href   string
	Active bool
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL environment variable is required\n")
		os.Exit(1)
	}

	outDir := "docs/dashboard"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	reposEnv := os.Getenv("DASHBOARD_REPOS")
	if reposEnv == "" {
		// Fall back to single-repo env var for backwards compatibility
		repo := os.Getenv("DASHBOARD_REPO")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		reposEnv = repo
	}

	repos := strings.Split(reposEnv, ",")
	for i := range repos {
		repos[i] = strings.TrimSpace(repos[i])
	}

	ctx := context.Background()

	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)

	tmplPath := filepath.Join(filepath.Dir(os.Args[0]), "template.html")
	if _, err := os.Stat(tmplPath); err != nil {
		tmplPath = "cmd/dashboard/template.html"
	}

	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse template: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	generated := time.Now().UTC().Format(time.RFC3339)

	// Build nav tabs and filename mapping
	type repoFile struct {
		repo     string
		filename string
	}
	var repoFiles []repoFile
	for i, repo := range repos {
		filename := repoSlug(repo) + ".html"
		if i == 0 {
			filename = "index.html"
		}
		repoFiles = append(repoFiles, repoFile{repo: repo, filename: filename})
	}

	for _, rf := range repoFiles {
		stats, err := s.GetDashboardStats(ctx, rf.repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get stats for %s: %v\n", rf.repo, err)
			os.Exit(1)
		}

		statsJSON, err := json.Marshal(stats)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to marshal stats: %v\n", err)
			os.Exit(1)
		}

		var navItems []RepoNav
		for _, other := range repoFiles {
			// Use short label: just the repo name after the owner
			label := other.repo
			if parts := strings.SplitN(other.repo, "/", 2); len(parts) == 2 {
				label = parts[1]
			}
			navItems = append(navItems, RepoNav{
				Label:  label,
				Href:   other.filename,
				Active: other.repo == rf.repo,
			})
		}

		outPath := filepath.Join(outDir, rf.filename)
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create %s: %v\n", outPath, err)
			os.Exit(1)
		}

		data := struct {
			Repo      string
			Generated string
			StatsJSON template.JS
			Repos     []RepoNav
		}{
			Repo:      rf.repo,
			Generated: generated,
			StatsJSON: template.JS(statsJSON),
			Repos:     navItems,
		}

		if err := tmpl.Execute(f, data); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "failed to render template for %s: %v\n", rf.repo, err)
			os.Exit(1)
		}
		f.Close()

		fmt.Printf("dashboard written to %s\n", outPath)
	}
}

// repoSlug converts "owner/repo" to "owner-repo" for use as a filename.
func repoSlug(repo string) string {
	return strings.ReplaceAll(repo, "/", "-")
}
