package synthesis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

const upstreamSimilarityThreshold = 0.45

// UpstreamSynthesizer detects upstream dependency changes that may impact
// project decisions (ADRs, roadmap items, research docs).
type UpstreamSynthesizer struct {
	store *store.Store
}

// NewUpstreamSynthesizer creates a new UpstreamSynthesizer.
func NewUpstreamSynthesizer(s *store.Store) *UpstreamSynthesizer {
	return &UpstreamSynthesizer{store: s}
}

// Name returns the synthesizer identifier.
func (u *UpstreamSynthesizer) Name() string {
	return "upstream_impact"
}

// Analyze finds recently ingested upstream docs and checks whether they relate
// to any existing ADR/roadmap/research documents via vector similarity.
func (u *UpstreamSynthesizer) Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	since := time.Now().Add(-window)
	recentUpstream, err := u.store.RecentDocumentsByType(ctx, repo, store.UpstreamDocTypes, since)
	if err != nil {
		return nil, fmt.Errorf("querying recent upstream docs: %w", err)
	}

	var findings []Finding
	for _, doc := range recentUpstream {
		if len(doc.Embedding) == 0 {
			continue
		}

		similar, err := u.store.FindSimilarDocuments(ctx, repo, store.EnhancementDocTypes, doc.Embedding, 5)
		if err != nil {
			return nil, fmt.Errorf("finding similar docs for %q: %w", doc.Title, err)
		}

		for _, match := range similar {
			if match.Distance >= upstreamSimilarityThreshold {
				continue
			}

			severity := "info"
			if isDeferredOrRejected(match.Content) {
				severity = "action_needed"
			}

			findings = append(findings, Finding{
				Type:     "upstream_signal",
				Severity: severity,
				Title:    fmt.Sprintf("Upstream change %q may impact %q", doc.Title, match.Title),
				Evidence: []string{
					fmt.Sprintf("upstream: %s (type: %s)", doc.Title, doc.DocType),
					fmt.Sprintf("related: %s (type: %s, distance: %.3f)", match.Title, match.DocType, match.Distance),
				},
				Suggestion: fmt.Sprintf("Review %q in light of upstream change %q.", match.Title, doc.Title),
			})
		}
	}

	return findings, nil
}

// isDeferredOrRejected checks whether document content mentions "deferred" or
// "rejected", indicating a decision that upstream changes might reopen.
func isDeferredOrRejected(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "deferred") || strings.Contains(lower, "rejected")
}
