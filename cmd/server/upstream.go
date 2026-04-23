package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/config"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/ingest"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/upstream"
)

// upstreamTarget is a single (installation, consumer repo, upstream repo) triple
// that the watcher should Sync + cross-reference.
type upstreamTarget struct {
	InstallationID int64
	ConsumerRepo   string
	UpstreamRepo   string
}

// upstreamWatchHandler runs the upstream watcher across all configured
// installations. Expects POST; authentication is applied by the caller when
// registering the handler (see main.go).
//
// Request body (optional): {"repo": "owner/name"} limits the run to a single
// consumer repo. Empty body processes every repo with an Upstream config.
func (srv *server) upstreamWatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	var req struct {
		Repo string `json:"repo"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("bad body: %v", err), http.StatusBadRequest)
			return
		}
	}

	targets, err := srv.resolveUpstreamTargets(ctx, req.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	watcher := upstream.NewWatcher(srv.gh, srv.store).
		WithIndexer(srv.upstreamIndexer()).
		WithBlockedFinder(srv.store, srv.llm)

	type upstreamMatch struct {
		ReleaseTag string `json:"release_tag"`
		Issues     []int  `json:"candidate_issues"`
	}
	type result struct {
		Repo    string          `json:"repo"`
		Synced  int             `json:"synced"`
		Matches []upstreamMatch `json:"matches"`
	}
	out := make([]result, 0, len(targets))
	for _, t := range targets {
		matches, err := watcher.SyncAndCrossReference(ctx, t.InstallationID, t.ConsumerRepo, t.UpstreamRepo)
		if err != nil {
			http.Error(w, fmt.Sprintf("sync %s: %v", t.ConsumerRepo, err), http.StatusInternalServerError)
			return
		}
		mm := make([]upstreamMatch, 0, len(matches))
		for _, m := range matches {
			nums := make([]int, 0, len(m.Candidates))
			for _, c := range m.Candidates {
				nums = append(nums, c.Number)
			}
			mm = append(mm, upstreamMatch{ReleaseTag: m.Release.TagName, Issues: nums})
		}
		out = append(out, result{Repo: t.ConsumerRepo, Synced: len(matches), Matches: mm})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"results": out}); err != nil {
		srv.logger.Error("encoding upstream-watch response", "error", err)
	}
}

// resolveUpstreamTargets enumerates installations and, for each repo the
// server is configured to handle, loads butler.json to find Upstream entries.
// Only repos where ResearchBrief.Enabled is true AND the Upstream list is
// non-empty are included. When filterRepo is non-empty, output is limited to
// that consumer repo.
func (srv *server) resolveUpstreamTargets(ctx context.Context, filterRepo string) ([]upstreamTarget, error) {
	ids, err := srv.gh.ListInstallations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var out []upstreamTarget
	for _, installID := range ids {
		for repo := range srv.allowedRepos {
			if filterRepo != "" && repo != filterRepo {
				continue
			}
			cfg, err := srv.loadButlerConfig(ctx, installID, repo)
			if err != nil {
				srv.logger.Warn("loading butler.json", "error", err, "repo", repo, "installation", installID)
				continue
			}
			if !cfg.ResearchBrief.Enabled || len(cfg.Upstream) == 0 {
				continue
			}
			for _, dep := range cfg.Upstream {
				if dep.Repo == "" {
					continue
				}
				out = append(out, upstreamTarget{
					InstallationID: installID,
					ConsumerRepo:   repo,
					UpstreamRepo:   dep.Repo,
				})
			}
		}
	}
	return out, nil
}

// loadButlerConfig fetches .github/butler.json for the given (installation,
// repo) pair and parses it. Returns the default config when the file does
// not exist. Fetch errors propagate so the caller can log per-repo.
func (srv *server) loadButlerConfig(ctx context.Context, installationID int64, repo string) (config.ButlerConfig, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := srv.gh.GetFileContents(fetchCtx, installationID, repo, ".github/butler.json")
	if err != nil {
		return config.ButlerConfig{}, err
	}
	if data == nil {
		return config.DefaultConfig(), nil
	}
	return config.Parse(data)
}

// upstreamIndexer adapts the existing ingest.EmbedAndUpsert pipeline into the
// upstream.Indexer interface the watcher expects.
func (srv *server) upstreamIndexer() upstream.Indexer {
	return upstream.IngestAdapter{EmbedFunc: func(ctx context.Context, doc store.Document) error {
		return ingest.EmbedAndUpsert(ctx, srv.store, srv.llm, doc)
	}}
}
