package main

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

//go:embed template.html
var rawDashboardTemplate string

func mustParseDashboard() *template.Template {
	return template.Must(template.New("dashboard").Parse(rawDashboardTemplate))
}

type dashboardNav struct {
	Label  string
	Href   string
	Active bool
}

func newDashboardHandler(s *store.Store, allowedRepos map[string]bool, repos []string, tmpl *template.Template, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = "IsmaelMartinez/teams-for-linux"
		}
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		stats, err := s.GetDashboardStats(r.Context(), repo)
		if err != nil {
			http.Error(w, "failed to get stats", http.StatusInternalServerError)
			return
		}

		statsJSON, err := json.Marshal(stats)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		navItems := make([]dashboardNav, 0, len(repos))
		for _, rr := range repos {
			label := rr
			if parts := strings.SplitN(rr, "/", 2); len(parts) == 2 {
				label = parts[1]
			}
			navItems = append(navItems, dashboardNav{
				Label:  label,
				Href:   "/dashboard?repo=" + rr,
				Active: rr == repo,
			})
		}

		data := struct {
			Repo      string
			Generated string
			StatsJSON template.JS
			Repos     []dashboardNav
		}{
			Repo:      repo,
			Generated: time.Now().UTC().Format(time.RFC3339),
			StatsJSON: template.JS(statsJSON),
			Repos:     navItems,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			logger.Error("rendering dashboard", "error", err)
		}
	}
}
