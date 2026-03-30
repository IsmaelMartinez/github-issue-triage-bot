//go:build integration

package synthesis

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// ---------------------------------------------------------------------------
// Helpers (Task 2)
// ---------------------------------------------------------------------------

func testDB(t *testing.T) *store.Store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/triage_bot?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := store.ConnectPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	s := store.New(pool)
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("pinging test database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return s
}

func cleanRepo(t *testing.T, s *store.Store, repo string) {
	t.Helper()
	ctx := context.Background()
	tables := []string{"issues", "documents", "repo_events", "doc_references", "bot_comments"}
	for _, tbl := range tables {
		_, err := s.Pool().Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE repo = $1", tbl), repo)
		if err != nil {
			t.Fatalf("cleaning table %s for repo %s: %v", tbl, repo, err)
		}
	}
}

func makeEmbedding(angle float64) []float32 {
	emb := make([]float32, store.EmbeddingDim)
	emb[0] = float32(math.Cos(angle))
	emb[1] = float32(math.Sin(angle))
	return emb
}

func backdateDocument(t *testing.T, s *store.Store, repo, title string, when time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := s.Pool().Exec(ctx,
		"UPDATE documents SET updated_at = $1 WHERE repo = $2 AND title = $3",
		when, repo, title)
	if err != nil {
		t.Fatalf("backdating document %q: %v", title, err)
	}
}

// seedOldReference inserts a doc_reference with a backdated created_at.
func seedOldReference(t *testing.T, s *store.Store, repo, sourceID, targetID string, when time.Time) {
	t.Helper()
	_, err := s.Pool().Exec(context.Background(),
		`INSERT INTO doc_references (repo, source_type, source_id, target_type, target_id, relationship, created_at)
		 VALUES ($1, 'issue', $2, 'document', $3, 'references', $4)
		 ON CONFLICT (repo, source_type, source_id, target_type, target_id, relationship) DO NOTHING`,
		repo, sourceID, targetID, when)
	if err != nil {
		t.Fatalf("seeding old reference for %s: %v", targetID, err)
	}
}

type mockIssueCreator struct {
	mu    sync.Mutex
	calls []mockCreateCall
}

type mockCreateCall struct {
	InstallationID int64
	Repo           string
	Title          string
	Body           string
}

func (m *mockIssueCreator) CreateIssue(_ context.Context, installationID int64, repo, title, body string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCreateCall{
		InstallationID: installationID,
		Repo:           repo,
		Title:          title,
		Body:           body,
	})
	return len(m.calls), nil
}

func (m *mockIssueCreator) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockIssueCreator) lastCall() mockCreateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[len(m.calls)-1]
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func filterByType(findings []Finding, typ string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TestClusterDetection (Task 3)
// ---------------------------------------------------------------------------

func TestClusterDetection(t *testing.T) {
	s := testDB(t)
	repo := "test/cluster-detection"
	t.Cleanup(func() { cleanRepo(t, s, repo) })
	cleanRepo(t, s, repo)

	ctx := context.Background()
	now := time.Now()

	// Seed 5 issues: 3 similar (angles 0.0, 0.05, 0.08), 2 distant (1.2, 2.0).
	issues := []struct {
		number int
		title  string
		angle  float64
	}{
		{1, "Audio cuts out during calls", 0.0},
		{2, "Sound drops in meetings", 0.05},
		{3, "No audio after screen share", 0.08},
		{4, "Dark mode button missing", 1.2},
		{5, "Login timeout on slow networks", 2.0},
	}
	for _, iss := range issues {
		err := s.UpsertIssue(ctx, store.Issue{
			Repo:      repo,
			Number:    iss.number,
			Title:     iss.title,
			Summary:   iss.title,
			State:     "open",
			Embedding: makeEmbedding(iss.angle),
			CreatedAt: now.Add(-24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("seeding issue #%d: %v", iss.number, err)
		}
	}

	cs := NewClusterSynthesizer(s)
	findings, err := cs.Analyze(ctx, repo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	clusters := filterByType(findings, "cluster")
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster finding, got %d: %v", len(clusters), clusters)
	}
	if len(clusters[0].Evidence) != 3 {
		t.Errorf("expected 3 issues in cluster, got %d: %v", len(clusters[0].Evidence), clusters[0].Evidence)
	}
	if clusters[0].Severity != "action_needed" {
		t.Errorf("expected severity action_needed (no ADR coverage), got %q", clusters[0].Severity)
	}

	t.Run("with_adr_coverage", func(t *testing.T) {
		// Add a cross-reference from issue #1 to "ADR-audio".
		err := s.RecordReferences(ctx, repo, "document", "ADR-audio", []store.DocReference{
			{TargetType: "issue", TargetID: "#1", Relationship: "references"},
		})
		if err != nil {
			t.Fatalf("recording reference: %v", err)
		}

		findings, err := cs.Analyze(ctx, repo, 7*24*time.Hour)
		if err != nil {
			t.Fatalf("Analyze with ADR: %v", err)
		}

		clusters := filterByType(findings, "cluster")
		if len(clusters) != 1 {
			t.Fatalf("expected 1 cluster finding, got %d", len(clusters))
		}
		if clusters[0].Severity != "warning" {
			t.Errorf("expected severity warning (with ADR coverage), got %q", clusters[0].Severity)
		}
	})

	t.Run("below_min_size", func(t *testing.T) {
		smallRepo := "test/cluster-small"
		t.Cleanup(func() { cleanRepo(t, s, smallRepo) })
		cleanRepo(t, s, smallRepo)

		// Only 2 similar issues — below min cluster size of 3.
		for i, angle := range []float64{0.0, 0.05} {
			err := s.UpsertIssue(ctx, store.Issue{
				Repo:      smallRepo,
				Number:    i + 1,
				Title:     fmt.Sprintf("Similar issue %d", i+1),
				Summary:   "similar",
				State:     "open",
				Embedding: makeEmbedding(angle),
				CreatedAt: now.Add(-24 * time.Hour),
			})
			if err != nil {
				t.Fatalf("seeding small repo issue: %v", err)
			}
		}

		cs := NewClusterSynthesizer(s)
		findings, err := cs.Analyze(ctx, smallRepo, 7*24*time.Hour)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("expected 0 findings for below-min-size, got %d", len(findings))
		}
	})
}

// ---------------------------------------------------------------------------
// TestStalenessDetection (Task 4)
// ---------------------------------------------------------------------------

func TestStalenessDetection(t *testing.T) {
	s := testDB(t)
	repo := "test/staleness-detection"
	t.Cleanup(func() { cleanRepo(t, s, repo) })
	cleanRepo(t, s, repo)

	ctx := context.Background()
	now := time.Now()

	// Seed 5 documents at various ages.
	docs := []struct {
		title   string
		docType string
		ageDays int
	}{
		{"ADR-001 Audio pipeline", "adr", 100},
		{"ADR-002 Caching strategy", "adr", 60},
		{"Roadmap Q3 goals", "roadmap", 40},
		{"Roadmap Q4 targets", "roadmap", 20},
		{"ADR-003 Notification system", "adr", 100},
	}
	for _, d := range docs {
		err := s.UpsertDocument(ctx, store.Document{
			Repo:      repo,
			DocType:   d.docType,
			Title:     d.title,
			Content:   "Content for " + d.title,
			Metadata:  map[string]any{},
			Embedding: makeEmbedding(0),
		})
		if err != nil {
			t.Fatalf("seeding document %q: %v", d.title, err)
		}
		backdateDocument(t, s, repo, d.title, now.Add(-time.Duration(d.ageDays)*24*time.Hour))
	}

	// Seed recent references (created_at = now) for docs that should be flagged.
	// Doc 0: "ADR-001 Audio pipeline" — 2 recent refs → should flag.
	for i := 0; i < 2; i++ {
		err := s.RecordReferences(ctx, repo, "issue", fmt.Sprintf("issue-ADR-001 Audio pipeline-%d", i), []store.DocReference{
			{TargetType: "document", TargetID: "ADR-001 Audio pipeline", Relationship: "references"},
		})
		if err != nil {
			t.Fatalf("seeding reference for ADR-001: %v", err)
		}
	}
	// Doc 2: "Roadmap Q3 goals" — 2 recent refs → should flag.
	for i := 0; i < 2; i++ {
		err := s.RecordReferences(ctx, repo, "issue", fmt.Sprintf("issue-Roadmap Q3 goals-%d", i), []store.DocReference{
			{TargetType: "document", TargetID: "Roadmap Q3 goals", Relationship: "references"},
		})
		if err != nil {
			t.Fatalf("seeding reference for Roadmap Q3: %v", err)
		}
	}

	// Seed old references (outside 7-day window) for docs that should NOT be flagged.
	oldRefTime := now.Add(-30 * 24 * time.Hour)

	// Doc 1: "ADR-002 Caching strategy" — 3 old refs.
	for i := 0; i < 3; i++ {
		seedOldReference(t, s, repo, fmt.Sprintf("issue-ADR-002-%d", i), "ADR-002 Caching strategy", oldRefTime)
	}
	// Doc 3: "Roadmap Q4 targets" — 5 old refs.
	for i := 0; i < 5; i++ {
		seedOldReference(t, s, repo, fmt.Sprintf("issue-Roadmap-Q4-%d", i), "Roadmap Q4 targets", oldRefTime)
	}
	// Doc 4: "ADR-003 Notification system" — 1 old ref.
	seedOldReference(t, s, repo, "issue-ADR-003-0", "ADR-003 Notification system", oldRefTime)

	ds := NewDriftSynthesizer(s)
	findings, err := ds.Analyze(ctx, repo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	stale := filterByType(findings, "staleness")
	if len(stale) != 2 {
		t.Fatalf("expected 2 staleness findings, got %d: %v", len(stale), stale)
	}

	// Verify the correct documents were flagged.
	flaggedTitles := map[string]bool{}
	for _, f := range stale {
		flaggedTitles[f.Title] = true
	}
	for _, expected := range []string{"ADR-001 Audio pipeline", "Roadmap Q3 goals"} {
		found := false
		for title := range flaggedTitles {
			if strings.Contains(title, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected staleness finding for %q, got titles: %v", expected, flaggedTitles)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDriftDetection (Task 5)
// ---------------------------------------------------------------------------

func TestDriftDetection(t *testing.T) {
	s := testDB(t)
	repo := "test/drift-detection"
	t.Cleanup(func() { cleanRepo(t, s, repo) })
	cleanRepo(t, s, repo)

	ctx := context.Background()
	now := time.Now()

	// Seed an ADR backdated 14 days.
	err := s.UpsertDocument(ctx, store.Document{
		Repo:      repo,
		DocType:   "adr",
		Title:     "ADR-005 Authentication flow",
		Content:   "Describes the authentication architecture.",
		Metadata:  map[string]any{},
		Embedding: makeEmbedding(0),
	})
	if err != nil {
		t.Fatalf("seeding ADR: %v", err)
	}
	backdateDocument(t, s, repo, "ADR-005 Authentication flow", now.Add(-14*24*time.Hour))

	// Seed two PR events.
	err = s.RecordEvents(ctx, []store.RepoEvent{
		{
			Repo:      repo,
			EventType: "pr_merged",
			SourceRef: "#42",
			Summary:   "Refactor auth handler",
			Metadata:  map[string]any{"changed_files": []any{"authentication/handler.go"}},
		},
		{
			Repo:      repo,
			EventType: "pr_merged",
			SourceRef: "#99",
			Summary:   "Update readme",
			Metadata:  map[string]any{"changed_files": []any{"docs/readme.md"}},
		},
	})
	if err != nil {
		t.Fatalf("seeding events: %v", err)
	}

	ds := NewDriftSynthesizer(s)
	findings, err := ds.Analyze(ctx, repo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	drifts := filterByType(findings, "drift")
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift finding, got %d: %v", len(drifts), drifts)
	}

	f := drifts[0]
	if !strings.Contains(f.Title, "#42") {
		t.Errorf("expected drift finding to reference PR #42, got title: %q", f.Title)
	}
	if !strings.Contains(f.Title, "ADR-005") {
		t.Errorf("expected drift finding to reference ADR-005, got title: %q", f.Title)
	}
}

// ---------------------------------------------------------------------------
// TestUpstreamImpact (Task 6)
// ---------------------------------------------------------------------------

func TestUpstreamImpact(t *testing.T) {
	s := testDB(t)
	repo := "test/upstream-impact"
	t.Cleanup(func() { cleanRepo(t, s, repo) })
	cleanRepo(t, s, repo)

	ctx := context.Background()
	now := time.Now()

	// Seed upstream release doc at angle 0.0.
	err := s.UpsertDocument(ctx, store.Document{
		Repo:      repo,
		DocType:   "upstream_release",
		Title:     "Electron v39.0.0",
		Content:   "Major changes to IPC and renderer process.",
		Metadata:  map[string]any{},
		Embedding: makeEmbedding(0.0),
	})
	if err != nil {
		t.Fatalf("seeding upstream doc: %v", err)
	}
	// Backdate to 2 days ago so it falls within the analysis window.
	backdateDocument(t, s, repo, "Electron v39.0.0", now.Add(-2*24*time.Hour))

	// Seed three ADRs at varying angles.
	adrs := []struct {
		title   string
		angle   float64
		content string
	}{
		{"ADR-007 IPC Architecture", 0.15, "Describes the IPC architecture for main-renderer communication."},
		{"ADR-009 Deferred WebRTC", 0.2, "WebRTC support deferred until upstream stabilizes."},
		{"ADR-010 CSS Theming", 1.2, "CSS theming approach for dark and light modes."},
	}
	for _, adr := range adrs {
		err := s.UpsertDocument(ctx, store.Document{
			Repo:      repo,
			DocType:   "adr",
			Title:     adr.title,
			Content:   adr.content,
			Metadata:  map[string]any{},
			Embedding: makeEmbedding(adr.angle),
		})
		if err != nil {
			t.Fatalf("seeding ADR %q: %v", adr.title, err)
		}
	}

	us := NewUpstreamSynthesizer(s)
	findings, err := us.Analyze(ctx, repo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	upstream := filterByType(findings, "upstream_signal")
	if len(upstream) != 2 {
		t.Fatalf("expected 2 upstream findings, got %d: %v", len(upstream), upstream)
	}

	// Verify both expected findings are present with correct severities.
	var adr007found, adr009found bool
	for _, f := range upstream {
		if strings.Contains(f.Title, "ADR-007") {
			adr007found = true
			if f.Severity != "info" {
				t.Errorf("ADR-007 finding should have severity info, got %q", f.Severity)
			}
		}
		if strings.Contains(f.Title, "ADR-009") {
			adr009found = true
			if f.Severity != "action_needed" {
				t.Errorf("ADR-009 finding should have severity action_needed, got %q", f.Severity)
			}
		}
	}
	if !adr007found {
		t.Error("expected a finding for ADR-007, but none was found")
	}
	if !adr009found {
		t.Error("expected a finding for ADR-009, but none was found")
	}
}

// ---------------------------------------------------------------------------
// TestRunnerQuietWeek (Task 7a)
// ---------------------------------------------------------------------------

func TestRunnerQuietWeek(t *testing.T) {
	s := testDB(t)
	repo := "test/runner-quiet"
	shadowRepo := "test/runner-quiet-shadow"
	t.Cleanup(func() {
		cleanRepo(t, s, repo)
		cleanRepo(t, s, shadowRepo)
	})
	cleanRepo(t, s, repo)
	cleanRepo(t, s, shadowRepo)

	ctx := context.Background()
	now := time.Now()

	// Seed a 100-day-old ADR with 2 recent refs. This produces staleness
	// findings with severity "info", which are not actionable.
	err := s.UpsertDocument(ctx, store.Document{
		Repo:      repo,
		DocType:   "adr",
		Title:     "ADR-050 Logging format",
		Content:   "Logging format decisions.",
		Metadata:  map[string]any{},
		Embedding: makeEmbedding(0),
	})
	if err != nil {
		t.Fatalf("seeding ADR: %v", err)
	}
	backdateDocument(t, s, repo, "ADR-050 Logging format", now.Add(-100*24*time.Hour))

	for i := 0; i < 2; i++ {
		err := s.RecordReferences(ctx, repo, "issue", fmt.Sprintf("issue-ADR-050-%d", i), []store.DocReference{
			{TargetType: "document", TargetID: "ADR-050 Logging format", Relationship: "references"},
		})
		if err != nil {
			t.Fatalf("seeding reference: %v", err)
		}
	}

	mock := &mockIssueCreator{}
	logger := testLogger()
	runner := NewRunner(mock, s, logger,
		NewClusterSynthesizer(s),
		NewDriftSynthesizer(s),
		NewUpstreamSynthesizer(s),
	)

	count, err := runner.Run(ctx, 1, repo, shadowRepo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 findings returned for quiet week, got %d", count)
	}
	if mock.callCount() != 0 {
		t.Errorf("expected mock not called for quiet week, got %d calls", mock.callCount())
	}
}

// ---------------------------------------------------------------------------
// TestRunnerPostsBriefing (Task 7b)
// ---------------------------------------------------------------------------

func TestRunnerPostsBriefing(t *testing.T) {
	s := testDB(t)
	repo := "test/runner-briefing"
	shadowRepo := "test/runner-briefing-shadow"
	t.Cleanup(func() {
		cleanRepo(t, s, repo)
		cleanRepo(t, s, shadowRepo)
	})
	cleanRepo(t, s, repo)

	ctx := context.Background()
	now := time.Now()

	// Seed 3 similar issues to produce an action_needed cluster finding.
	for i, angle := range []float64{0.0, 0.05, 0.08} {
		err := s.UpsertIssue(ctx, store.Issue{
			Repo:      repo,
			Number:    i + 1,
			Title:     fmt.Sprintf("Performance regression %d", i+1),
			Summary:   "App is slow",
			State:     "open",
			Embedding: makeEmbedding(angle),
			CreatedAt: now.Add(-24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("seeding issue: %v", err)
		}
	}

	mock := &mockIssueCreator{}
	logger := testLogger()
	runner := NewRunner(mock, s, logger,
		NewClusterSynthesizer(s),
		NewDriftSynthesizer(s),
		NewUpstreamSynthesizer(s),
	)

	count, err := runner.Run(ctx, 1, repo, shadowRepo, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count == 0 {
		t.Fatal("expected non-zero findings count")
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected mock called once, got %d", mock.callCount())
	}

	call := mock.lastCall()
	if !strings.Contains(call.Title, "[Briefing] Weekly") {
		t.Errorf("expected title to contain '[Briefing] Weekly', got %q", call.Title)
	}
	if !strings.Contains(call.Body, "Emerging Patterns") {
		t.Errorf("expected body to contain 'Emerging Patterns', got body length %d", len(call.Body))
	}
}
