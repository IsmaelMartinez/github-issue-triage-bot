package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/hats"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/regression"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// workingVersionRe captures a prior working version phrase like "works in
// v1.2.3" or "worked in 1.2". Anchors the regression-window PR diff in Step 6
// of the handler.
var workingVersionRe = regexp.MustCompile(`(?i)(?:works\s+in|worked\s+in|working\s+on|prior\s+working)\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

type briefPreviewRequest struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	HatName     string `json:"hat,omitempty"`
}

type briefPreviewResponse struct {
	Class              string                  `json:"class"`
	SimilarIssues      []store.SimilarIssue    `json:"similar_past_issues"`
	Docs               []store.SimilarDocument `json:"docs"`
	RegressionPRs      []regression.PRSummary  `json:"regression_prs"`
	UpstreamCandidates []int                   `json:"upstream_candidates"`
}

// issuePayload is the minimal GitHub issue shape we need for the smoke test.
// Scoped to this file because there is no project-wide issue fetcher yet;
// the rest of the bot consumes issue data from webhook payloads.
type issuePayload struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
}

// briefPreviewHandler validates that every retrieval piece the research-brief
// bot depends on — issue fetch, embedding, hats taxonomy, document search,
// past-issue search, regression PR diff, blocked-issue cross-reference —
// works end-to-end against a real issue. No LLM generation, no brief.
//
// On retrieval failures after the issue is fetched, the field is left empty
// rather than returning 500. Hard failures (issue fetch, embed) return 500.
func (srv *server) briefPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req briefPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Repo == "" || req.IssueNumber == 0 {
		http.Error(w, "repo and issue_number required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	installID, err := srv.installationIDFor(ctx, req.Repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("installation: %v", err), http.StatusInternalServerError)
		return
	}

	// 1. Fetch the issue via GitHub API. The store has no GetIssue helper,
	// so we hit /repos/{owner}/{repo}/issues/{number} directly.
	issue, err := srv.fetchIssueForPreview(ctx, installID, req.Repo, req.IssueNumber)
	if err != nil {
		http.Error(w, fmt.Sprintf("get issue: %v", err), http.StatusInternalServerError)
		return
	}

	// 2. Embed title + body so the three retrieval calls share one vector.
	vec, err := srv.llm.Embed(ctx, issue.Title+"\n\n"+issue.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("embed: %v", err), http.StatusInternalServerError)
		return
	}

	// 3. Load hats.md for this repo. Construct a fresh loader per request —
	// /brief-preview is manual/smoke only, so cache reuse is not worth
	// complicating the server struct.
	var hat *hats.Hat
	cfg, cfgErr := srv.loadButlerConfig(ctx, installID, req.Repo)
	if cfgErr != nil {
		srv.logger.Warn("brief-preview: loading butler.json", "error", cfgErr, "repo", req.Repo)
	} else {
		fetch := hats.GitHubFetchFunc(ctx, srv.gh, installID, req.Repo, cfg.ResearchBrief.HatsPath)
		loader := hats.NewLoader(fetch, 5*time.Minute)
		tax, hatsErr := loader.Get()
		if hatsErr != nil {
			srv.logger.Warn("brief-preview: loading hats.md", "error", hatsErr, "repo", req.Repo)
		} else if req.HatName != "" {
			hat = tax.Find(req.HatName)
		}
	}

	// 4. Similar docs, with a soft rerank when the caller named a hat.
	docs, docsErr := srv.store.FindSimilarDocuments(ctx, req.Repo, allDocTypes(), vec, 5)
	if docsErr != nil {
		srv.logger.Warn("brief-preview: similar docs", "error", docsErr, "repo", req.Repo)
		docs = nil
	}
	if hat != nil {
		docs = store.ApplyHatBoost(docs, hat.RetrievalBoostKeywords, 0.05)
	}

	// 5. Similar past issues.
	similar, simErr := srv.store.FindSimilarIssues(ctx, req.Repo, vec, req.IssueNumber, 5)
	if simErr != nil {
		srv.logger.Warn("brief-preview: similar issues", "error", simErr, "repo", req.Repo)
		similar = nil
	}

	// 6. Regression-window PR diff: only runs when the issue body names a
	// prior working version. Resolve current from the latest release tag.
	var prs []regression.PRSummary
	if m := workingVersionRe.FindStringSubmatch(issue.Body); m != nil {
		working := "v" + m[1]
		releases, relErr := srv.gh.GetLatestReleases(ctx, installID, req.Repo, 1)
		if relErr != nil {
			srv.logger.Warn("brief-preview: resolve latest release", "error", relErr, "repo", req.Repo)
		} else if len(releases) > 0 {
			keywords := extractSymptomKeywords(issue.Body)
			diff := regression.NewDiff(srv.gh)
			found, runErr := diff.Run(ctx, installID, req.Repo, working, releases[0].TagName, keywords)
			if runErr != nil {
				srv.logger.Warn("brief-preview: regression diff", "error", runErr, "repo", req.Repo, "working", working, "current", releases[0].TagName)
			} else {
				prs = make([]regression.PRSummary, 0, len(found))
				for _, p := range found {
					prs = append(prs, regression.PRSummary{Number: p.Number, Title: p.Title, URL: p.URL})
				}
			}
		}
	}

	// 7. Upstream candidates: open `blocked` issues near this issue's embedding.
	var upstreamNums []int
	blocked, blkErr := srv.store.FindSimilarBlockedIssues(ctx, req.Repo, vec, 3)
	if blkErr != nil {
		srv.logger.Warn("brief-preview: blocked issues", "error", blkErr, "repo", req.Repo)
	} else {
		upstreamNums = make([]int, 0, len(blocked))
		for _, b := range blocked {
			upstreamNums = append(upstreamNums, b.Number)
		}
	}

	resp := briefPreviewResponse{
		Class:              className(hat, req.HatName),
		SimilarIssues:      similar,
		Docs:               docs,
		RegressionPRs:      prs,
		UpstreamCandidates: upstreamNums,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		srv.logger.Error("brief-preview: encode response", "error", err)
	}
}

// fetchIssueForPreview retrieves an issue by (repo, number) via the GitHub
// REST API. Scoped to this file because no other call site needs it yet —
// the webhook flow gets issue data from the event payload, and the agent
// flow creates/mirrors rather than fetching.
func (srv *server) fetchIssueForPreview(ctx context.Context, installationID int64, repo string, number int) (*issuePayload, error) {
	token, err := srv.gh.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, fmt.Errorf("installation token: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
	}
	var issue issuePayload
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &issue, nil
}

// installationIDFor picks the first installation the GitHub App is installed
// on. Matches the pattern used by /cleanup, /health-check, and /synthesize —
// the bot currently runs as a single-installation deployment, so this is
// deliberately simple rather than filtering by repo ownership.
func (srv *server) installationIDFor(ctx context.Context, repo string) (int64, error) {
	ids, err := srv.gh.ListInstallations(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, errors.New("no installations")
	}
	return ids[0], nil
}

// className returns the hat name if the taxonomy lookup matched, the caller's
// requested name when they named one but the taxonomy did not have it, or
// "other" when no hat was requested. This keeps the smoke-test response
// honest about which path was taken.
func className(h *hats.Hat, requested string) string {
	if h != nil {
		return h.Name
	}
	if requested != "" {
		return requested
	}
	return "other"
}

// allDocTypes is the union of every doc_type the retrieval engine can read.
// Hard-coded rather than referencing store.AllSeedableDocTypes so this
// endpoint's contract is stable even if the seed universe changes.
func allDocTypes() []string {
	return []string{"troubleshooting", "configuration", "adr", "roadmap", "research", "upstream_release", "upstream_issue"}
}

// extractSymptomKeywords pulls a very small set of candidate keywords from
// the issue body to drive regression-window PR filtering. Kept deliberately
// naive — the real brief generator will do this via an LLM call.
func extractSymptomKeywords(body string) []string {
	candidates := []string{"iframe", "reload", "network", "auth", "wayland", "ozone", "camera", "screen", "tray", "notification", "sharepoint"}
	lowered := strings.ToLower(body)
	var hit []string
	for _, c := range candidates {
		if strings.Contains(lowered, c) {
			hit = append(hit, c)
		}
	}
	return hit
}
