package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL environment variable is required\n")
		os.Exit(1)
	}

	outPath := "docs/dashboard/index.html"
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}

	repo := os.Getenv("DASHBOARD_REPO")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}

	ctx := context.Background()

	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)

	stats, err := s.GetDashboardStats(ctx, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get dashboard stats: %v\n", err)
		os.Exit(1)
	}

	statsJSON, err := json.Marshal(stats)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal stats: %v\n", err)
		os.Exit(1)
	}

	tmplPath := filepath.Join(filepath.Dir(os.Args[0]), "template.html")
	// If the binary-relative path doesn't exist, try relative to the source.
	if _, err := os.Stat(tmplPath); err != nil {
		// When run via `go run`, use the source directory.
		tmplPath = "cmd/dashboard/template.html"
	}

	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse template: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	data := struct {
		Repo      string
		Generated string
		StatsJSON template.JS
	}{
		Repo:      repo,
		Generated: time.Now().UTC().Format(time.RFC3339),
		StatsJSON: template.JS(statsJSON),
	}

	if err := tmpl.Execute(f, data); err != nil {
		fmt.Fprintf(os.Stderr, "failed to render template: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("dashboard written to %s\n", outPath)
}
