package upstream

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

type fakeReleases struct {
	releases []gh.Release
	err      error
}

func (f *fakeReleases) GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]gh.Release, error) {
	return f.releases, f.err
}

type fakeEvents struct {
	existing map[string]bool
	recorded []store.RepoEvent
}

func (f *fakeEvents) ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]store.RepoEvent, error) {
	var out []store.RepoEvent
	for ref := range f.existing {
		out = append(out, store.RepoEvent{Repo: repo, EventType: "upstream_release", SourceRef: ref})
	}
	return out, nil
}

func (f *fakeEvents) RecordEvents(ctx context.Context, ev []store.RepoEvent) error {
	f.recorded = append(f.recorded, ev...)
	return nil
}

func TestWatcher_RecordsOnlyNewReleases(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v39.8.0", Name: "39.8.0", Body: "note a", PublishedAt: time.Now()},
		{TagName: "v39.8.1", Name: "39.8.1", Body: "note b", PublishedAt: time.Now()},
		{TagName: "v39.8.2", Name: "39.8.2", Body: "note c", PublishedAt: time.Now()},
	}
	events := &fakeEvents{existing: map[string]bool{"v39.8.0": true}}
	w := NewWatcher(&fakeReleases{releases: rel}, events)
	recorded, err := w.Sync(context.Background(), 1, "teams-for-linux", "electron/electron")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded = %d, want 2 new", len(recorded))
	}
	refs := []string{recorded[0].SourceRef, recorded[1].SourceRef}
	want := map[string]bool{"v39.8.1": true, "v39.8.2": true}
	for _, r := range refs {
		if !want[r] {
			t.Errorf("unexpected recorded ref %q", r)
		}
	}
}

func TestWatcher_NothingToDoIfAllKnown(t *testing.T) {
	rel := []gh.Release{{TagName: "v1.0.0"}}
	events := &fakeEvents{existing: map[string]bool{"v1.0.0": true}}
	w := NewWatcher(&fakeReleases{releases: rel}, events)
	recorded, err := w.Sync(context.Background(), 1, "r", "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 0 {
		t.Errorf("len = %d, want 0", len(recorded))
	}
}

func TestWatcher_PropagatesError(t *testing.T) {
	w := NewWatcher(&fakeReleases{err: errors.New("boom")}, &fakeEvents{})
	_, err := w.Sync(context.Background(), 1, "r", "u")
	if err == nil {
		t.Fatal("want error")
	}
}

type fakeIndexer struct {
	upserted []store.Document
}

func (f *fakeIndexer) UpsertEmbedded(ctx context.Context, doc store.Document) error {
	f.upserted = append(f.upserted, doc)
	return nil
}

func TestWatcher_EmbedsNewReleases(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v41.0.0", Name: "41.0.0", Body: "fixes input method", PublishedAt: time.Now()},
	}
	events := &fakeEvents{existing: map[string]bool{}}
	idx := &fakeIndexer{}
	w := NewWatcher(&fakeReleases{releases: rel}, events).WithIndexer(idx)
	if _, err := w.Sync(context.Background(), 1, "teams-for-linux", "electron/electron"); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(idx.upserted) != 1 {
		t.Fatalf("len(upserted) = %d, want 1", len(idx.upserted))
	}
	got := idx.upserted[0]
	if got.DocType != "upstream_release" {
		t.Errorf("doc_type = %q", got.DocType)
	}
	if got.Repo != "teams-for-linux" {
		t.Errorf("repo = %q", got.Repo)
	}
	if got.Title != "v41.0.0" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestWatcher_NoIndexerSkipsEmbedding(t *testing.T) {
	rel := []gh.Release{{TagName: "v1.0.0", Body: "x"}}
	events := &fakeEvents{existing: map[string]bool{}}
	w := NewWatcher(&fakeReleases{releases: rel}, events)
	if _, err := w.Sync(context.Background(), 1, "r", "u"); err != nil {
		t.Fatal(err)
	}
	// No indexer configured; test passes if no panic and the recorded event landed.
	if len(events.recorded) != 1 {
		t.Errorf("events recorded = %d, want 1", len(events.recorded))
	}
}

type failingIndexer struct {
	failAt   int // 1-based: fail on the Nth call
	upserted []store.Document
	calls    int
}

func (f *failingIndexer) UpsertEmbedded(ctx context.Context, doc store.Document) error {
	f.calls++
	if f.calls == f.failAt {
		return errors.New("transient embed failure")
	}
	f.upserted = append(f.upserted, doc)
	return nil
}

func TestWatcher_ContinuesEmbeddingOnError(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v1.0.0", Body: "a"},
		{TagName: "v1.0.1", Body: "b"},
		{TagName: "v1.0.2", Body: "c"},
	}
	events := &fakeEvents{existing: map[string]bool{}}
	idx := &failingIndexer{failAt: 2}
	w := NewWatcher(&fakeReleases{releases: rel}, events).WithIndexer(idx)
	fresh, err := w.Sync(context.Background(), 1, "r", "u")
	if err == nil {
		t.Fatal("want aggregated error")
	}
	if len(fresh) != 3 {
		t.Errorf("len(fresh) = %d, want 3 (journal still complete)", len(fresh))
	}
	if len(idx.upserted) != 2 {
		t.Errorf("upserted = %d, want 2 (one failure does not stop the loop)", len(idx.upserted))
	}
}

type fakeBlockedFinder struct {
	issues []store.SimilarIssue
}

func (f *fakeBlockedFinder) FindSimilarBlockedIssues(ctx context.Context, repo string, embedding []float32, limit int) ([]store.SimilarIssue, error) {
	return f.issues, nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, 768), nil
}

func TestWatcher_CrossReferencesBlockedIssues(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v39.8.2", Body: "fix VideoFrame prototype via contextBridge", PublishedAt: time.Now()},
	}
	bf := &fakeBlockedFinder{issues: []store.SimilarIssue{
		{Issue: store.Issue{Number: 2169, Title: "Camera broken"}, Distance: 0.20},
		{Issue: store.Issue{Number: 9999, Title: "Unrelated"}, Distance: 0.90},
	}}
	w := NewWatcher(&fakeReleases{releases: rel}, &fakeEvents{existing: map[string]bool{}}).
		WithBlockedFinder(bf, stubEmbedder{})
	matches, err := w.SyncAndCrossReference(context.Background(), 1, "teams-for-linux", "electron/electron")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Release.TagName != "v39.8.2" {
		t.Errorf("wrong release: %s", matches[0].Release.TagName)
	}
	if len(matches[0].Candidates) != 1 || matches[0].Candidates[0].Number != 2169 {
		t.Errorf("wrong candidates: %v", matches[0].Candidates)
	}
}

func TestWatcher_NoCrossReferenceWithoutDependencies(t *testing.T) {
	rel := []gh.Release{{TagName: "v1.0.0", Body: "x"}}
	events := &fakeEvents{existing: map[string]bool{}}
	w := NewWatcher(&fakeReleases{releases: rel}, events)
	matches, err := w.SyncAndCrossReference(context.Background(), 1, "r", "u")
	if err != nil {
		t.Fatal(err)
	}
	if matches != nil {
		t.Errorf("matches = %v, want nil (no deps)", matches)
	}
}
