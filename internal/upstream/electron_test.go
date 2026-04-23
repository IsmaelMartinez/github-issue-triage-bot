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
