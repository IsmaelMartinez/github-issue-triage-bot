// Package upstream watches upstream dependency releases and records them
// in the event journal for downstream cross-reference work.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// ReleaseLister is the subset of the github client we need.
type ReleaseLister interface {
	GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]gh.Release, error)
}

// EventStore is the subset of the store we need.
type EventStore interface {
	ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]store.RepoEvent, error)
	RecordEvents(ctx context.Context, events []store.RepoEvent) error
}

// Watcher pulls new upstream releases and records them against a consumer repo.
type Watcher struct {
	gh     ReleaseLister
	events EventStore
	idx    Indexer
	lookN  int
	window time.Duration
}

// NewWatcher constructs a Watcher with defaults (20 releases fetched per Sync,
// 180-day lookback for existing-event dedupe).
func NewWatcher(g ReleaseLister, e EventStore) *Watcher {
	return &Watcher{gh: g, events: e, lookN: 20, window: 180 * 24 * time.Hour}
}

// Sync fetches recent releases from upstreamRepo and records any that are
// not already in the consumerRepo's event journal as "upstream_release"
// events. Returns the slice of newly recorded events.
func (w *Watcher) Sync(ctx context.Context, installationID int64, consumerRepo, upstreamRepo string) ([]store.RepoEvent, error) {
	releases, err := w.gh.GetLatestReleases(ctx, installationID, upstreamRepo, w.lookN)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-w.window)
	existing, err := w.events.ListEvents(ctx, consumerRepo, since, []string{"upstream_release"}, 1000)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.SourceRef] = true
	}
	var fresh []store.RepoEvent
	for _, r := range releases {
		if seen[r.TagName] {
			continue
		}
		fresh = append(fresh, store.RepoEvent{
			Repo:      consumerRepo,
			EventType: "upstream_release",
			SourceRef: r.TagName,
			Summary:   r.Name,
			Metadata: map[string]any{
				"upstream_repo": upstreamRepo,
				"tag":           r.TagName,
				"prerelease":    r.Prerelease,
				"body":          r.Body,
				"html_url":      r.HTMLURL,
				"published_at":  r.PublishedAt,
			},
		})
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	if err := w.events.RecordEvents(ctx, fresh); err != nil {
		return nil, err
	}
	if w.idx != nil {
		var errs []error
		for _, ev := range fresh {
			body, _ := ev.Metadata["body"].(string)
			tag, _ := ev.Metadata["tag"].(string)
			doc := store.Document{
				Repo:     ev.Repo,
				DocType:  "upstream_release",
				Title:    tag,
				Content:  body,
				Metadata: ev.Metadata,
			}
			if err := w.idx.UpsertEmbedded(ctx, doc); err != nil {
				errs = append(errs, fmt.Errorf("embed %s: %w", tag, err))
			}
		}
		if len(errs) > 0 {
			return fresh, errors.Join(errs...)
		}
	}
	return fresh, nil
}

// Indexer handles embedding and upserting a document into the vector store.
// A small interface so tests can stub it without pulling in LLM or store
// concrete types.
type Indexer interface {
	UpsertEmbedded(ctx context.Context, doc store.Document) error
}

// WithIndexer sets an optional indexer. When set, Sync also embeds and
// upserts the release notes as a Document with doc_type "upstream_release".
func (w *Watcher) WithIndexer(i Indexer) *Watcher {
	w.idx = i
	return w
}

// IngestAdapter bridges the existing ingest.EmbedAndUpsert into the Indexer
// interface without the upstream package depending on ingest directly.
type IngestAdapter struct {
	EmbedFunc func(ctx context.Context, doc store.Document) error
}

func (a IngestAdapter) UpsertEmbedded(ctx context.Context, doc store.Document) error {
	return a.EmbedFunc(ctx, doc)
}
