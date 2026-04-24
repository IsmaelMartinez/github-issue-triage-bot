// Package regression runs a keyword-filtered diff of merged PRs between
// two release tags to surface candidate causes for a regression report.
package regression

import (
	"context"
	"strings"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

// PRLister is the subset of the github client this package needs.
type PRLister interface {
	ListMergedPRsBetween(ctx context.Context, installationID int64, repo, from, to string) ([]gh.MergedPR, error)
}

// Diff runs regression-window analysis.
type Diff struct {
	client PRLister
}

// NewDiff constructs a Diff backed by the given PRLister.
func NewDiff(c PRLister) *Diff {
	return &Diff{client: c}
}

// PRSummary is a compact shape for downstream handlers that do not need
// the full MergedPR fields.
type PRSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// Run returns PRs merged between fromTag and toTag whose title or body
// contains at least one of the given keywords (case-insensitive). If
// keywords is nil or empty, all PRs are returned unfiltered.
func (d *Diff) Run(ctx context.Context, installationID int64, repo, fromTag, toTag string, keywords []string) ([]gh.MergedPR, error) {
	prs, err := d.client.ListMergedPRsBetween(ctx, installationID, repo, fromTag, toTag)
	if err != nil {
		return nil, err
	}
	if len(keywords) == 0 {
		return prs, nil
	}
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = strings.ToLower(k)
	}
	out := make([]gh.MergedPR, 0, len(prs))
	for _, p := range prs {
		hay := strings.ToLower(p.Title + " " + p.Body)
		for _, k := range lowered {
			if strings.Contains(hay, k) {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}
