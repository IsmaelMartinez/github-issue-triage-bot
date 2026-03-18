//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// These tests require a running PostgreSQL with migrations applied.
// Run: docker-compose up -d && go test -tags integration ./internal/store/ -v
// DATABASE_URL defaults to the docker-compose setup if not set.

func testDB(t *testing.T) *store.Store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/triage_bot?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := store.ConnectPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	s := store.New(pool)
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return s
}

func TestEventJournalIntegration(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	repo := "integration-test/event-journal"

	// Clean up any leftover test data
	t.Cleanup(func() {
		_, _ = s.Pool().Exec(ctx, "DELETE FROM repo_events WHERE repo = $1", repo)
	})

	// Record events
	events := []store.RepoEvent{
		{Repo: repo, EventType: "issue_opened", SourceRef: "#1", Summary: "Test issue opened", Areas: []string{"test"}, Metadata: map[string]any{"labels": []string{"bug"}}},
		{Repo: repo, EventType: "issue_closed", SourceRef: "#1", Summary: "Test issue closed"},
		{Repo: repo, EventType: "pr_merged", SourceRef: "#10", Summary: "Test PR merged", Areas: []string{"internal/store"}},
	}
	if err := s.RecordEvents(ctx, events); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}

	// Verify count
	count, err := s.CountEvents(ctx, repo)
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if count != 3 {
		t.Errorf("CountEvents = %d, want 3", count)
	}

	// List all events
	listed, err := s.ListEvents(ctx, repo, time.Now().Add(-1*time.Hour), nil, 100)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(listed) != 3 {
		t.Errorf("ListEvents returned %d events, want 3", len(listed))
	}

	// List filtered by type
	issueEvents, err := s.ListEvents(ctx, repo, time.Now().Add(-1*time.Hour), []string{"issue_opened"}, 100)
	if err != nil {
		t.Fatalf("ListEvents filtered: %v", err)
	}
	if len(issueEvents) != 1 {
		t.Errorf("ListEvents(issue_opened) returned %d events, want 1", len(issueEvents))
	}
	if issueEvents[0].SourceRef != "#1" {
		t.Errorf("SourceRef = %q, want %q", issueEvents[0].SourceRef, "#1")
	}

	// Verify metadata round-trips
	meta, _ := json.Marshal(issueEvents[0].Metadata)
	if string(meta) == "{}" || string(meta) == "null" {
		t.Error("metadata should contain labels, got empty")
	}

	// Cleanup works
	deleted, err := s.CleanupOldEvents(ctx, 0)
	if err != nil {
		t.Fatalf("CleanupOldEvents: %v", err)
	}
	if deleted != 3 {
		t.Errorf("CleanupOldEvents deleted %d, want 3", deleted)
	}
}

func TestCrossReferenceIntegration(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	repo := "integration-test/cross-refs"

	t.Cleanup(func() {
		_, _ = s.Pool().Exec(ctx, "DELETE FROM doc_references WHERE repo = $1", repo)
	})

	// Extract references from content
	content := "See #42 for the bug report. As per ADR-007, we decided against WebRTC. Related: #42 again."
	refs := store.ExtractReferences(content)
	if len(refs) != 2 {
		t.Fatalf("ExtractReferences returned %d refs, want 2 (deduplicated)", len(refs))
	}

	// Record references
	if err := s.RecordReferences(ctx, repo, "document", "docs/adr/007.md", refs); err != nil {
		t.Fatalf("RecordReferences: %v", err)
	}

	// Record same references again — should not fail (ON CONFLICT DO NOTHING)
	if err := s.RecordReferences(ctx, repo, "document", "docs/adr/007.md", refs); err != nil {
		t.Fatalf("RecordReferences (duplicate): %v", err)
	}

	// Find references to issue #42
	found, err := s.FindReferencesTo(ctx, repo, "issue", "#42")
	if err != nil {
		t.Fatalf("FindReferencesTo: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("FindReferencesTo(#42) returned %d refs, want 1", len(found))
	}

	// Count references to ADR-007
	count, err := s.CountReferencesTo(ctx, repo, "document", "ADR-007")
	if err != nil {
		t.Fatalf("CountReferencesTo: %v", err)
	}
	if count != 1 {
		t.Errorf("CountReferencesTo(ADR-007) = %d, want 1", count)
	}
}

func TestIngestEndpointIntegration(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	repo := "integration-test/ingest"

	t.Cleanup(func() {
		_, _ = s.Pool().Exec(ctx, "DELETE FROM repo_events WHERE repo = $1", repo)
	})

	// Simulate what the /ingest endpoint does: batch insert events
	events := []store.RepoEvent{
		{Repo: repo, EventType: "pr_merged", SourceRef: "#100", Summary: "Merged: fix audio bug", Metadata: map[string]any{"user": "testuser"}},
		{Repo: repo, EventType: "release_published", SourceRef: "v1.0.0", Summary: "Release v1.0.0", Metadata: map[string]any{"tag": "v1.0.0"}},
	}
	if err := s.RecordEvents(ctx, events); err != nil {
		t.Fatalf("RecordEvents (ingest simulation): %v", err)
	}

	count, err := s.CountEvents(ctx, repo)
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if count != 2 {
		t.Errorf("CountEvents = %d, want 2", count)
	}
}
