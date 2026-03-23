# Repository Strategist Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the triage bot into a repository strategist with institutional memory, pattern detection, and strategic intelligence.

**Architecture:** Three monthly batches build on each other: Month 1 adds the event journal, auto-ingestion, cross-reference index, and per-repo config. Month 2 adds the synthesis engine with three synthesizers (cluster detection, drift detection, upstream impact). Month 3 adds strategic output (roadmap proposals, ADR lifecycle, state-of-project briefings) and multi-repo hardening.

**Tech Stack:** Go 1.26, PostgreSQL + pgvector (Neon), Gemini 2.5 Flash, GitHub App API, GitHub Actions (cron triggers)

**Spec:** `docs/plans/2026-03-18-repository-strategist-design.md`

---

## Batch 1: Institutional Memory (Month 1)

### Task 1: Per-Repo Config — Butler JSON Parser

**Files:**
- Create: `internal/config/butler.go`
- Create: `internal/config/butler_test.go`

This is the foundation — everything else in months 1-3 reads this config.

- [ ] **Step 1: Write failing tests for config parsing**

```go
// internal/config/butler_test.go
package config

import "testing"

func TestParseButlerConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ButlerConfig
		wantErr bool
	}{
		{
			name:  "full config",
			input: `{"capabilities":{"triage":true,"synthesis":true,"auto_ingest":true},"doc_paths":["docs/**"],"shadow_repo":"owner/shadow","max_daily_llm_calls":50}`,
			want: ButlerConfig{
				Capabilities: Capabilities{Triage: true, Synthesis: true, AutoIngest: true},
				DocPaths:     []string{"docs/**"},
				ShadowRepo:   "owner/shadow",
				MaxDailyLLMCalls: 50,
			},
		},
		{
			name:  "empty YAML returns defaults",
			input: "",
			want:  DefaultConfig(),
		},
		{
			name:    "invalid JSON",
			input:   `{"bad":"json"`,
			wantErr: true,
		},
		{
			name:  "missing capabilities uses defaults",
			input: `{"shadow_repo":"owner/shadow"}`,
			want: func() ButlerConfig {
				c := DefaultConfig()
				c.ShadowRepo = "owner/shadow"
				return c
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.ShadowRepo != tt.want.ShadowRepo {
				t.Errorf("ShadowRepo = %q, want %q", got.ShadowRepo, tt.want.ShadowRepo)
			}
			if got.Capabilities.Synthesis != tt.want.Capabilities.Synthesis {
				t.Errorf("Synthesis = %v, want %v", got.Capabilities.Synthesis, tt.want.Capabilities.Synthesis)
			}
			if got.MaxDailyLLMCalls != tt.want.MaxDailyLLMCalls {
				t.Errorf("MaxDailyLLMCalls = %d, want %d", got.MaxDailyLLMCalls, tt.want.MaxDailyLLMCalls)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.Capabilities.Triage {
		t.Error("default should enable triage")
	}
	if c.Capabilities.Synthesis {
		t.Error("default should disable synthesis")
	}
	if c.MaxDailyLLMCalls != 50 {
		t.Errorf("default MaxDailyLLMCalls = %d, want 50", c.MaxDailyLLMCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement config types and parser**

```go
// internal/config/butler.go
package config

import "encoding/json"

type ButlerConfig struct {
	Capabilities     Capabilities      `json:"capabilities"`
	DocPaths         []string          `json:"doc_paths"`
	Upstream         []UpstreamDep     `json:"upstream"`
	Synthesis        SynthesisConfig   `json:"synthesis"`
	ShadowRepo       string            `json:"shadow_repo"`
	Thresholds       map[string]float64 `json:"thresholds"`
	MaxDailyLLMCalls int               `json:"max_daily_llm_calls"`
}

type Capabilities struct {
	Triage     bool `json:"triage"`
	Research   bool `json:"research"`
	Synthesis  bool `json:"synthesis"`
	AutoIngest bool `json:"auto_ingest"`
}

type UpstreamDep struct {
	Repo    string `json:"repo"`
	DocType string `json:"doc_type"`
	Track   string `json:"track"`
}

type SynthesisConfig struct {
	Frequency string `json:"frequency"`
	Day       string `json:"day"`
}

func DefaultConfig() ButlerConfig {
	return ButlerConfig{
		Capabilities:     Capabilities{Triage: true, Research: true},
		DocPaths:         []string{"docs/**", "*.md"},
		Synthesis:        SynthesisConfig{Frequency: "weekly", Day: "monday"},
		MaxDailyLLMCalls: 50,
		Thresholds: map[string]float64{
			"troubleshooting":  0.70,
			"adr":              0.55,
			"roadmap":          0.55,
			"research":         0.55,
			"configuration":    0.50,
			"upstream_release": 0.45,
			"upstream_issue":   0.45,
		},
	}
}

func Parse(data []byte) (ButlerConfig, error) {
	cfg := DefaultConfig()
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ButlerConfig{}, err
	}
	if cfg.MaxDailyLLMCalls <= 0 {
		cfg.MaxDailyLLMCalls = 50
	}
	return cfg, nil
}
```

Note: the project prefers standard library where possible, but YAML parsing has no standard library option. Use `encoding/json` instead — change the config file format to `.github/butler.json` and use `encoding/json` for parsing. Update all references from `butler.json` to `butler.json` and from `yaml` tags to `json` tags. This avoids adding a new dependency.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/butler.go internal/config/butler_test.go go.mod go.sum
git commit -m "feat: add butler.json config parser with defaults"
```

---

### Task 2: Config Caching and GitHub Contents API Reader

**Files:**
- Create: `internal/config/loader.go`
- Create: `internal/config/loader_test.go`
- Modify: `internal/github/client.go` — add `GetFileContents` method

The config loader fetches `.github/butler.json` from the repo via the GitHub Contents API and caches it with a 1-hour TTL. This is called early in the webhook handler before event processing.

- [ ] **Step 1: Add `GetFileContents` to GitHub client**

Add to `internal/github/client.go` after the `SearchIssues` method (~line 509):

```go
// GetFileContents reads a file's contents from a repository via the Contents API.
// Returns the decoded content bytes and nil error, or nil bytes with nil error if the file does not exist (404).
func (c *Client) GetFileContents(ctx context.Context, installationID int64, repo, path string) ([]byte, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected encoding: %s", result.Encoding)
	}
	return base64.StdEncoding.DecodeString(result.Content)
}
```

Add `const maxErrorBodyBytes = 4096` near the top of the file if not already present.

- [ ] **Step 2: Write failing test for config loader with TTL cache**

```go
// internal/config/loader_test.go
package config

import (
	"testing"
	"time"
)

func TestConfigCache(t *testing.T) {
	calls := 0
	fetcher := func() ([]byte, error) {
		calls++
		return []byte(`{"capabilities":{"synthesis":true}}`), nil
	}

	cache := NewCache(1*time.Hour, fetcher)

	// First call fetches
	cfg1, err := cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg1.Capabilities.Synthesis {
		t.Error("expected synthesis enabled")
	}
	if calls != 1 {
		t.Errorf("expected 1 fetch call, got %d", calls)
	}

	// Second call hits cache
	_, err = cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected still 1 fetch call, got %d", calls)
	}
}

func TestConfigCacheReturnsDefaultOnNilContent(t *testing.T) {
	fetcher := func() ([]byte, error) {
		return nil, nil // file not found
	}
	cache := NewCache(1*time.Hour, fetcher)
	cfg, err := cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capabilities.Synthesis {
		t.Error("default config should have synthesis disabled")
	}
	if !cfg.Capabilities.Triage {
		t.Error("default config should have triage enabled")
	}
}
```

- [ ] **Step 3: Implement config cache**

```go
// internal/config/loader.go
package config

import (
	"sync"
	"time"
)

type FetchFunc func() ([]byte, error)

type Cache struct {
	mu      sync.RWMutex
	cfg     ButlerConfig
	fetched bool
	fetchAt time.Time
	ttl     time.Duration
	fetch   FetchFunc
}

func NewCache(ttl time.Duration, fetch FetchFunc) *Cache {
	return &Cache{ttl: ttl, fetch: fetch}
}

func (c *Cache) Get() (ButlerConfig, error) {
	c.mu.RLock()
	if c.fetched && time.Since(c.fetchAt) < c.ttl {
		cfg := c.cfg
		c.mu.RUnlock()
		return cfg, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.fetched && time.Since(c.fetchAt) < c.ttl {
		return c.cfg, nil
	}

	data, err := c.fetch()
	if err != nil {
		// On fetch error, return cached value if available
		if c.fetched {
			return c.cfg, nil
		}
		return DefaultConfig(), nil
	}

	if data == nil {
		c.cfg = DefaultConfig()
	} else {
		cfg, parseErr := Parse(data)
		if parseErr != nil {
			c.cfg = DefaultConfig()
		} else {
			c.cfg = cfg
		}
	}
	c.fetched = true
	c.fetchAt = time.Now()
	return c.cfg, nil
}

func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.fetched = false
	c.mu.Unlock()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/loader.go internal/config/loader_test.go internal/github/client.go
git commit -m "feat: add config cache with TTL and GitHub Contents API reader"
```

---

### Task 3: Event Journal — Migration and Store Methods

**Files:**
- Create: `migrations/011_repo_events.sql`
- Create: `internal/store/events.go`
- Create: `internal/store/events_test.go`

- [ ] **Step 1: Write migration**

```sql
-- migrations/011_repo_events.sql
CREATE TABLE repo_events (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    source_ref  TEXT,
    summary     TEXT NOT NULL,
    areas       TEXT[],
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_repo_events_repo_type ON repo_events(repo, event_type);
CREATE INDEX idx_repo_events_repo_created ON repo_events(repo, created_at DESC);
```

- [ ] **Step 2: Write failing tests for event store methods**

```go
// internal/store/events_test.go
package store

import "testing"

func TestRepoEventModel(t *testing.T) {
	e := RepoEvent{
		Repo:      "owner/repo",
		EventType: "issue_opened",
		SourceRef: "#42",
		Summary:   "New bug report about audio",
		Areas:     []string{"audio", "electron"},
		Metadata:  map[string]any{"labels": []string{"bug"}},
	}

	if e.Repo != "owner/repo" {
		t.Errorf("Repo = %q", e.Repo)
	}
	if e.EventType != "issue_opened" {
		t.Errorf("EventType = %q", e.EventType)
	}
	if len(e.Areas) != 2 {
		t.Errorf("Areas length = %d, want 2", len(e.Areas))
	}
}
```

Note: full database integration tests require a running PostgreSQL. Unit tests cover model shape and are sufficient for CI. Integration tests are added in Task 12 (month 3).

- [ ] **Step 3: Implement event store methods**

```go
// internal/store/events.go
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RepoEvent represents a recorded repository event in the journal.
type RepoEvent struct {
	ID        int64
	Repo      string
	EventType string
	SourceRef string
	Summary   string
	Areas     []string
	Metadata  map[string]any
	CreatedAt time.Time
}

// RecordEvent inserts a single event into the journal.
func (s *Store) RecordEvent(ctx context.Context, event RepoEvent) error {
	meta, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO repo_events (repo, event_type, source_ref, summary, areas, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.Repo, event.EventType, event.SourceRef, event.Summary, event.Areas, meta)
	if err != nil {
		return fmt.Errorf("insert repo event: %w", err)
	}
	return nil
}

// RecordEvents inserts a batch of events into the journal.
func (s *Store) RecordEvents(ctx context.Context, events []RepoEvent) error {
	for _, e := range events {
		if err := s.RecordEvent(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// ListEvents returns events for a repo within a time window, ordered by created_at DESC.
func (s *Store) ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]RepoEvent, error) {
	query := `
		SELECT id, repo, event_type, source_ref, summary, areas, metadata, created_at
		FROM repo_events
		WHERE repo = $1 AND created_at >= $2
	`
	args := []any{repo, since}

	if len(eventTypes) > 0 {
		query += ` AND event_type = ANY($3)`
		args = append(args, eventTypes)
		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args)+1)
		args = append(args, limit)
	} else {
		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args)+1)
		args = append(args, limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RepoEvent
	for rows.Next() {
		var e RepoEvent
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Repo, &e.EventType, &e.SourceRef, &e.Summary, &e.Areas, &meta, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &e.Metadata)
		results = append(results, e)
	}
	return results, rows.Err()
}

// CleanupOldEvents deletes events older than the given duration (retention policy).
func (s *Store) CleanupOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `DELETE FROM repo_events WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountEvents returns the total number of events for a repo.
func (s *Store) CountEvents(ctx context.Context, repo string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM repo_events WHERE repo = $1`, repo).Scan(&count)
	return count, err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -run TestRepoEvent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add migrations/011_repo_events.sql internal/store/events.go internal/store/events_test.go
git commit -m "feat: add event journal table and store methods"
```

---

### Task 4: Webhook Journal Layer — Record Events from Existing Handlers

**Files:**
- Modify: `internal/webhook/handler.go` — add journal writes to processEvent, processCommentEvent, handlePush

The webhook handler already processes issue events, comments, and push events. This task adds a thin layer that writes a journal entry alongside each existing handler, without changing any existing behaviour.

- [ ] **Step 1: Write test for event conversion helpers**

```go
// internal/webhook/journal_test.go
package webhook

import (
	"testing"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestIssueEventToRepoEvent(t *testing.T) {
	tests := []struct {
		name   string
		action string
		issue  gh.IssueDetail
		want   store.RepoEvent
	}{
		{
			name:   "opened bug",
			action: "opened",
			issue:  gh.IssueDetail{Number: 42, Title: "Audio broken", Labels: []gh.LabelInfo{{Name: "bug"}}},
			want: store.RepoEvent{
				EventType: "issue_opened",
				SourceRef: "#42",
				Summary:   "Audio broken",
			},
		},
		{
			name:   "closed issue",
			action: "closed",
			issue:  gh.IssueDetail{Number: 10, Title: "Old bug"},
			want: store.RepoEvent{
				EventType: "issue_closed",
				SourceRef: "#10",
				Summary:   "Old bug",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueToRepoEvent("owner/repo", tt.action, tt.issue)
			if got.EventType != tt.want.EventType {
				t.Errorf("EventType = %q, want %q", got.EventType, tt.want.EventType)
			}
			if got.SourceRef != tt.want.SourceRef {
				t.Errorf("SourceRef = %q, want %q", got.SourceRef, tt.want.SourceRef)
			}
			if got.Summary != tt.want.Summary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.want.Summary)
			}
		})
	}
}
```

- [ ] **Step 2: Implement journal helper and integration points**

Create `internal/webhook/journal.go`:

```go
package webhook

import (
	"context"
	"fmt"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func issueToRepoEvent(repo, action string, issue gh.IssueDetail) store.RepoEvent {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	return store.RepoEvent{
		Repo:      repo,
		EventType: "issue_" + action,
		SourceRef: fmt.Sprintf("#%d", issue.Number),
		Summary:   issue.Title,
		Metadata:  map[string]any{"labels": labels},
	}
}

func commentToRepoEvent(repo string, issueNumber int, user, body string) store.RepoEvent {
	summary := body
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return store.RepoEvent{
		Repo:      repo,
		EventType: "comment",
		SourceRef: fmt.Sprintf("#%d", issueNumber),
		Summary:   summary,
		Metadata:  map[string]any{"user": user},
	}
}

func pushToRepoEvent(repo, ref string) store.RepoEvent {
	return store.RepoEvent{
		Repo:      repo,
		EventType: "push",
		SourceRef: ref,
		Summary:   fmt.Sprintf("Push to %s", ref),
	}
}

func (h *Handler) recordEvent(ctx context.Context, event store.RepoEvent) {
	if err := h.store.RecordEvent(ctx, event); err != nil {
		h.logger.Error("recording event journal entry", "error", err, "eventType", event.EventType)
	}
}
```

Then add `h.recordEvent(ctx, ...)` calls to the existing handlers in `handler.go`:

In `processEvent` (~line 264), add at the start of the function:
```go
h.recordEvent(ctx, issueToRepoEvent(repo, event.Action, issue))
```

In `processCommentEvent` (~line 182), add after the bot check (~line 191):
```go
h.recordEvent(ctx, commentToRepoEvent(repo, issueNumber, commentUser, commentBody))
```

In `handlePush` (~line 495), the current code returns early when no shadow repo is configured. Restructure the function so that the journal write and auto-ingest (Task 8) happen for ALL repos, not just those with shadow repos. Move the journal write to the very start of the function, before the shadow repo check:
```go
h.recordEvent(ctx, pushToRepoEvent(repo, event.Ref))
```
Then restructure the shadow repo check so it only gates the mirror sync, not the entire function. The auto-ingest call (Task 8) goes after the mirror sync block, outside the shadow-repo guard.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/webhook/ -run TestIssueEventToRepoEvent -v`
Expected: PASS

- [ ] **Step 4: Run full test suite to verify no regressions**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/journal.go internal/webhook/journal_test.go internal/webhook/handler.go
git commit -m "feat: record webhook events in event journal"
```

---

### Task 5: Ingest Endpoint — Authenticated Batch Event Ingestion

**Files:**
- Modify: `cmd/server/main.go` — add `/ingest` endpoint and `INGEST_SECRET` env var
- Create: `cmd/server/ingest_test.go`

- [ ] **Step 1: Write test for ingest endpoint auth and parsing**

```go
// cmd/server/ingest_test.go
package main

import "testing"

func TestValidateIngestSecret(t *testing.T) {
	tests := []struct {
		name   string
		header string
		secret string
		want   bool
	}{
		{"valid", "Bearer mysecret", "mysecret", true},
		{"wrong secret", "Bearer wrong", "mysecret", false},
		{"missing bearer", "mysecret", "mysecret", false},
		{"empty header", "", "mysecret", false},
		{"empty secret disables auth", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateIngestAuth(tt.header, tt.secret); got != tt.want {
				t.Errorf("validateIngestAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Implement ingest endpoint**

Add to `cmd/server/main.go`:

The `validateIngestAuth` function (usable from the test):
```go
func validateIngestAuth(authHeader, secret string) bool {
	if secret == "" {
		return true
	}
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || authHeader[:len(prefix)] != prefix {
		return false
	}
	return authHeader[len(prefix):] == secret
}
```

The `/ingest` handler registered in the mux (after the `/health-check` handler, around line 257):
```go
ingestSecret := os.Getenv("INGEST_SECRET")
mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var events []store.RepoEvent
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := s.RecordEvents(r.Context(), events); err != nil {
		logger.Error("ingesting events", "error", err)
		http.Error(w, "ingest failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ingested":%d}`, len(events))
})
```

- [ ] **Step 3: Run tests**

Run: `go test ./cmd/server/ -run TestValidateIngestSecret -v`
Expected: PASS

- [ ] **Step 4: Add event cleanup to server startup**

In `cmd/server/main.go`, after the webhook delivery cleanup (~line 74), add:
```go
// Clean up old event journal entries (180-day retention)
if deleted, err := s.CleanupOldEvents(ctx, 180*24*time.Hour); err != nil {
	logger.Error("cleanup old events failed", "error", err)
} else if deleted > 0 {
	logger.Info("cleaned up old events", "deleted", deleted)
}
```

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go cmd/server/ingest_test.go
git commit -m "feat: add authenticated /ingest endpoint for batch event ingestion"
```

---

### Task 6: GitHub Action — Daily Event Scraper

**Files:**
- Create: `.github/workflows/event-ingest.yml`

This Action runs daily, queries the GitHub API for events the webhook doesn't see (PRs merged, releases, label changes), and POSTs them to the `/ingest` endpoint.

- [ ] **Step 1: Write the workflow file**

```yaml
# .github/workflows/event-ingest.yml
name: Daily Event Ingest

on:
  schedule:
    - cron: '0 5 * * *'  # 05:00 UTC daily (low-traffic time)
  workflow_dispatch: {}

jobs:
  ingest:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
      contents: read
    steps:
      - name: Fetch recent events from GitHub API
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          TARGET_REPO: IsmaelMartinez/teams-for-linux
        run: |
          # Fetch PRs merged in last 24 hours
          gh api "repos/${TARGET_REPO}/pulls?state=closed&sort=updated&direction=desc&per_page=50" \
            --jq '[.[] | select(.merged_at != null) | select((.merged_at | fromdateiso8601) > (now - 86400)) | {
              repo: env.TARGET_REPO,
              event_type: "pr_merged",
              source_ref: ("#" + (.number | tostring)),
              summary: .title,
              areas: [.head.ref],
              metadata: {pr_number: .number, changed_files: .changed_files, user: .user.login}
            }]' > /tmp/events.json

          # Fetch releases published in last 24 hours
          gh api "repos/${TARGET_REPO}/releases?per_page=10" \
            --jq '[.[] | select((.published_at | fromdateiso8601) > (now - 86400)) | {
              repo: env.TARGET_REPO,
              event_type: "release_published",
              source_ref: .tag_name,
              summary: .name,
              metadata: {tag: .tag_name, prerelease: .prerelease}
            }]' > /tmp/releases.json

          # Combine
          jq -s 'add // []' /tmp/events.json /tmp/releases.json > /tmp/all_events.json
          echo "Events to ingest: $(jq length /tmp/all_events.json)"

      - name: Post events to ingest endpoint
        env:
          INGEST_SECRET: ${{ secrets.INGEST_SECRET }}
          CLOUD_RUN_URL: ${{ secrets.CLOUD_RUN_URL }}
        run: |
          EVENT_COUNT=$(jq length /tmp/all_events.json)
          if [ "$EVENT_COUNT" -eq 0 ]; then
            echo "No events to ingest"
            exit 0
          fi
          curl -sf -X POST "${CLOUD_RUN_URL}/ingest" \
            -H "Authorization: Bearer ${INGEST_SECRET}" \
            -H "Content-Type: application/json" \
            -d @/tmp/all_events.json
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/event-ingest.yml
git commit -m "feat: add daily GitHub Action for event ingestion"
```

---

### Task 7: Extract Shared Ingest Package from Seed CLI

**Files:**
- Create: `internal/ingest/embed.go`
- Create: `internal/ingest/embed_test.go`
- Modify: `cmd/seed/main.go` — refactor to use `internal/ingest`

This extracts the core embedding + document upsert logic into a shared package that both the seed CLI and the webhook auto-ingest can call.

- [ ] **Step 1: Write test for the shared embed function**

```go
// internal/ingest/embed_test.go
package ingest

import "testing"

func TestDocFromRawContent(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		path    string
		content string
		want    string // expected doc_type
	}{
		{"ADR file", "owner/repo", "docs/adr/001-foo.md", "# ADR 001", "adr"},
		{"research file", "owner/repo", "docs/research/bar.md", "# Research", "research"},
		{"roadmap file", "owner/repo", "docs/plan/roadmap.md", "# Roadmap", "roadmap"},
		{"troubleshooting", "owner/repo", "docs/troubleshooting/fix.md", "# Fix", "troubleshooting"},
		{"generic markdown", "owner/repo", "README.md", "# Project", "configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := DocFromRawContent(tt.repo, tt.path, tt.content)
			if doc.DocType != tt.want {
				t.Errorf("DocType = %q, want %q", doc.DocType, tt.want)
			}
			if doc.Repo != tt.repo {
				t.Errorf("Repo = %q, want %q", doc.Repo, tt.repo)
			}
		})
	}
}
```

- [ ] **Step 2: Implement shared ingest package**

```go
// internal/ingest/embed.go
package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// DocFromRawContent creates a Document from a file path and its raw markdown content.
// The doc_type is inferred from the file path.
func DocFromRawContent(repo, path, content string) store.Document {
	return store.Document{
		Repo:    repo,
		DocType: inferDocType(path),
		Title:   path,
		Content: content,
		Metadata: map[string]any{
			"doc_path": path,
		},
	}
}

// EmbedAndUpsert embeds a document and upserts it into the store.
func EmbedAndUpsert(ctx context.Context, s *store.Store, l llm.Provider, doc store.Document) error {
	text := fmt.Sprintf("%s\n%s", doc.Title, doc.Content)
	if len(text) > 2000 {
		text = text[:2000]
	}
	embedding, err := l.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed %q: %w", doc.Title, err)
	}
	doc.Embedding = embedding
	return s.UpsertDocument(ctx, doc)
}

func inferDocType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "adr"):
		return "adr"
	case strings.Contains(lower, "research"):
		return "research"
	case strings.Contains(lower, "roadmap") || strings.Contains(lower, "plan"):
		return "roadmap"
	case strings.Contains(lower, "troubleshoot"):
		return "troubleshooting"
	default:
		return "configuration"
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/ingest/ -v`
Expected: PASS

- [ ] **Step 4: Refactor seed CLI to use shared package**

In `cmd/seed/main.go`, replace the inline embedding calls in `seedFeatures` and `seedTroubleshooting` with calls to `ingest.EmbedAndUpsert`. The seed CLI remains functional — this is a refactor, not a behaviour change. Keep the rate limiting logic (`time.Sleep`) in the seed CLI since it's specific to batch operations.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ingest/embed.go internal/ingest/embed_test.go cmd/seed/main.go
git commit -m "refactor: extract shared ingest package from seed CLI"
```

---

### Task 8: Auto-Ingest on Push — Embed Changed Docs

**Files:**
- Modify: `internal/webhook/handler.go` — extend `handlePush` to detect doc changes and auto-embed
- Modify: `internal/github/client.go` — add method to get file content by path (already done in Task 2)

The push webhook already fires for default branch pushes. This adds logic to check if any changed files match the configured `doc_paths`, fetch their content, and embed them.

- [ ] **Step 1: Add `Commits` field to PushEvent**

In `internal/github/client.go`, extend the `PushEvent` struct (~line 457):

```go
type PushEvent struct {
	Ref          string           `json:"ref"`
	Commits      []PushCommit     `json:"commits"`
	Repo         RepoDetail       `json:"repository"`
	Installation InstallationInfo `json:"installation"`
}

type PushCommit struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}
```

- [ ] **Step 2: Write test for doc path matching**

```go
// internal/webhook/autoingest_test.go
package webhook

import "testing"

func TestMatchesDocPaths(t *testing.T) {
	paths := []string{"docs/**", "*.md", "ADR-*"}
	tests := []struct {
		file string
		want bool
	}{
		{"docs/adr/001.md", true},
		{"docs/research/foo.md", true},
		{"README.md", true},
		{"ADR-007.md", true},
		{"src/main.go", false},
		{"internal/store/events.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			if got := matchesDocPaths(tt.file, paths); got != tt.want {
				t.Errorf("matchesDocPaths(%q) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 3: Implement auto-ingest in push handler**

Create `internal/webhook/autoingest.go`:

```go
package webhook

import (
	"context"
	"path/filepath"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/ingest"
)

func matchesDocPaths(file string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, file); matched {
			return true
		}
		if matched, _ := filepath.Match(p, filepath.Base(file)); matched {
			return true
		}
		// Handle docs/** by checking if file starts with the prefix before **
		if idx := len(p) - 2; idx > 0 && p[idx:] == "**" {
			prefix := p[:idx]
			if len(file) >= len(prefix) && file[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

func (h *Handler) autoIngestDocs(ctx context.Context, installationID int64, repo string, commits []gh.PushCommit, docPaths []string) {
	seen := make(map[string]bool)
	var toIngest []string

	for _, c := range commits {
		for _, f := range append(c.Added, c.Modified...) {
			if !seen[f] && matchesDocPaths(f, docPaths) {
				seen[f] = true
				toIngest = append(toIngest, f)
			}
		}
	}

	if len(toIngest) == 0 {
		return
	}

	h.logger.Info("auto-ingesting changed docs", "repo", repo, "count", len(toIngest))
	for _, path := range toIngest {
		content, err := h.github.GetFileContents(ctx, installationID, repo, path)
		if err != nil {
			h.logger.Error("fetching doc for auto-ingest", "error", err, "path", path)
			continue
		}
		if content == nil {
			continue
		}
		doc := ingest.DocFromRawContent(repo, path, string(content))
		if err := ingest.EmbedAndUpsert(ctx, h.store, h.llm, doc); err != nil {
			h.logger.Error("auto-ingesting doc", "error", err, "path", path)
		}
	}
}
```

Then in `handlePush` in `handler.go` (~line 495), after the mirror sync, add the auto-ingest call. This requires access to the butler config — pass it through or load it here. For now, use a hardcoded default until the config loading is wired into the handler (Task 10).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/webhook/ -run TestMatchesDocPaths -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/autoingest.go internal/webhook/autoingest_test.go internal/webhook/handler.go internal/github/client.go
git commit -m "feat: auto-ingest changed docs on push to default branch"
```

---

### Task 9: Cross-Reference Index — Migration and Extraction

**Files:**
- Create: `migrations/012_doc_references.sql`
- Create: `internal/store/references.go`
- Create: `internal/store/references_test.go`

- [ ] **Step 1: Write migration**

```sql
-- migrations/012_doc_references.sql
CREATE TABLE doc_references (
    id           BIGSERIAL PRIMARY KEY,
    repo         TEXT NOT NULL,
    source_type  TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    TEXT NOT NULL,
    relationship TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo, source_type, source_id, target_type, target_id, relationship)
);
CREATE INDEX idx_doc_refs_source ON doc_references(repo, source_type, source_id);
CREATE INDEX idx_doc_refs_target ON doc_references(repo, target_type, target_id);
```

- [ ] **Step 2: Write tests for reference extraction**

```go
// internal/store/references_test.go
package store

import "testing"

func TestExtractReferences(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []DocReference
	}{
		{
			name:    "issue references",
			content: "See #42 and #123 for details",
			want: []DocReference{
				{TargetType: "issue", TargetID: "#42", Relationship: "references"},
				{TargetType: "issue", TargetID: "#123", Relationship: "references"},
			},
		},
		{
			name:    "ADR references",
			content: "As decided in ADR-007, we use WebRTC. See also ADR-12.",
			want: []DocReference{
				{TargetType: "document", TargetID: "ADR-007", Relationship: "references"},
				{TargetType: "document", TargetID: "ADR-12", Relationship: "references"},
			},
		},
		{
			name:    "no references",
			content: "Just some plain text with no refs",
			want:    nil,
		},
		{
			name:    "mixed references",
			content: "See #42, related to ADR-003",
			want: []DocReference{
				{TargetType: "issue", TargetID: "#42", Relationship: "references"},
				{TargetType: "document", TargetID: "ADR-003", Relationship: "references"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractReferences(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractReferences() returned %d refs, want %d", len(got), len(tt.want))
			}
			for i, ref := range got {
				if ref.TargetType != tt.want[i].TargetType || ref.TargetID != tt.want[i].TargetID {
					t.Errorf("ref[%d] = {%s, %s}, want {%s, %s}", i, ref.TargetType, ref.TargetID, tt.want[i].TargetType, tt.want[i].TargetID)
				}
			}
		})
	}
}
```

- [ ] **Step 3: Implement reference extraction and store methods**

```go
// internal/store/references.go
package store

import (
	"context"
	"regexp"
)

// DocReference represents a cross-reference between documents, issues, or PRs.
type DocReference struct {
	ID           int64
	Repo         string
	SourceType   string
	SourceID     string
	TargetType   string
	TargetID     string
	Relationship string
}

var (
	issueRefRe = regexp.MustCompile(`#(\d+)`)
	adrRefRe   = regexp.MustCompile(`ADR-(\d+)`)
)

// ExtractReferences finds issue and ADR references in text content using regex.
func ExtractReferences(content string) []DocReference {
	seen := make(map[string]bool)
	var refs []DocReference

	for _, match := range issueRefRe.FindAllString(content, -1) {
		if !seen[match] {
			seen[match] = true
			refs = append(refs, DocReference{
				TargetType:   "issue",
				TargetID:     match,
				Relationship: "references",
			})
		}
	}

	for _, match := range adrRefRe.FindAllString(content, -1) {
		if !seen[match] {
			seen[match] = true
			refs = append(refs, DocReference{
				TargetType:   "document",
				TargetID:     match,
				Relationship: "references",
			})
		}
	}

	return refs
}

// RecordReferences inserts cross-references for a source document or issue.
func (s *Store) RecordReferences(ctx context.Context, repo, sourceType, sourceID string, refs []DocReference) error {
	for _, ref := range refs {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO doc_references (repo, source_type, source_id, target_type, target_id, relationship)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING
		`, repo, sourceType, sourceID, ref.TargetType, ref.TargetID, ref.Relationship)
		if err != nil {
			return err
		}
	}
	return nil
}

// FindReferencesTo returns all documents/issues that reference the given target.
func (s *Store) FindReferencesTo(ctx context.Context, repo, targetType, targetID string) ([]DocReference, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, source_type, source_id, target_type, target_id, relationship
		FROM doc_references
		WHERE repo = $1 AND target_type = $2 AND target_id = $3
	`, repo, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []DocReference
	for rows.Next() {
		var r DocReference
		if err := rows.Scan(&r.ID, &r.Repo, &r.SourceType, &r.SourceID, &r.TargetType, &r.TargetID, &r.Relationship); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// CountReferencesTo returns the count of references to a given target in a time window.
func (s *Store) CountReferencesTo(ctx context.Context, repo, targetType, targetID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM doc_references
		WHERE repo = $1 AND target_type = $2 AND target_id = $3
	`, repo, targetType, targetID).Scan(&count)
	return count, err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -run TestExtractReferences -v`
Expected: PASS

- [ ] **Step 5: Wire reference extraction into document upsert**

In `internal/ingest/embed.go`, extend `EmbedAndUpsert` to extract and record references after upserting:

```go
func EmbedAndUpsert(ctx context.Context, s *store.Store, l llm.Provider, doc store.Document) error {
	// ... existing embedding and upsert code ...

	// Extract and record cross-references
	refs := store.ExtractReferences(doc.Content)
	if len(refs) > 0 {
		_ = s.RecordReferences(ctx, doc.Repo, "document", doc.Title, refs)
	}
	return nil
}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add migrations/012_doc_references.sql internal/store/references.go internal/store/references_test.go internal/ingest/embed.go
git commit -m "feat: add cross-reference index with regex extraction"
```

---

### Task 10: Wire Config into Webhook Handler

**Files:**
- Modify: `internal/webhook/handler.go` — add config cache to Handler struct
- Modify: `cmd/server/main.go` — pass config dependencies to handler

This wires the per-repo config loading into the webhook handler so all existing and new features can read the butler config.

- [ ] **Step 1: Add config cache map to Handler struct**

In `internal/webhook/handler.go`, add to the Handler struct (~line 32):
```go
configCaches map[string]*config.Cache // keyed by repo
configMu     sync.Mutex
```

Add a method to get or create a config cache for a repo:
```go
func (h *Handler) getConfig(ctx context.Context, installationID int64, repo string) config.ButlerConfig {
	h.configMu.Lock()
	cache, ok := h.configCaches[repo]
	if !ok {
		cache = config.NewCache(1*time.Hour, func() ([]byte, error) {
			// Use a fresh background context with timeout — the cached fetcher
			// outlives the original request context
			fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return h.github.GetFileContents(fetchCtx, installationID, repo, ".github/butler.json")
		})
		h.configCaches[repo] = cache
	}
	h.configMu.Unlock()

	cfg, _ := cache.Get()
	return cfg
}
```

Initialize `configCaches` in `New()`:
```go
configCaches: make(map[string]*config.Cache),
```

- [ ] **Step 2: Use config in handlePush for auto-ingest**

In `handlePush`, after the mirror sync, add:
```go
cfg := h.getConfig(ctx, event.Installation.ID, repo)
if cfg.Capabilities.AutoIngest {
	h.autoIngestDocs(ctx, event.Installation.ID, repo, event.Commits, cfg.DocPaths)
}
```

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/webhook/handler.go cmd/server/main.go
git commit -m "feat: wire per-repo butler config into webhook handler"
```

---

## Batch 2: Pattern Detection Engine (Month 2) [DONE]

All 5 tasks (11-15) completed in PR #75. Synthesis engine with three synthesizers, /synthesize endpoint, and weekly cron workflow.

### Task 11: Synthesis Engine Skeleton — Types, Runner, Briefing Builder [DONE]

**Files:**
- Create: `internal/synthesis/types.go`
- Create: `internal/synthesis/runner.go`
- Create: `internal/synthesis/briefing.go`
- Create: `internal/synthesis/briefing_test.go`
- Modify: `cmd/server/main.go` — add `/synthesize` endpoint

- [ ] **Step 1: Write tests for briefing builder**

```go
// internal/synthesis/briefing_test.go
package synthesis

import (
	"strings"
	"testing"
)

func TestBuildBriefing(t *testing.T) {
	findings := []Finding{
		{Type: "cluster", Severity: "warning", Title: "Audio issues cluster", Evidence: []string{"#42", "#43"}, Suggestion: "Consider investigating"},
		{Type: "staleness", Severity: "info", Title: "Roadmap item R-3 inactive", Evidence: []string{"roadmap.md"}, Suggestion: "Review priority"},
	}

	md := BuildBriefing("2026-04-20", findings)

	if len(md) == 0 {
		t.Fatal("empty briefing")
	}
	if !strings.Contains(md, "[Briefing]") {
		t.Error("missing briefing title")
	}
	if !strings.Contains(md, "Audio issues cluster") {
		t.Error("missing cluster finding")
	}
	if !strings.Contains(md, "Roadmap item R-3") {
		t.Error("missing staleness finding")
	}
}

func TestBuildBriefingEmpty(t *testing.T) {
	md := BuildBriefing("2026-04-20", nil)
	if !strings.Contains(md, "Quiet week") {
		t.Error("empty briefing should mention quiet week")
	}
}
```

- [ ] **Step 2: Implement types, runner, and briefing builder**

`internal/synthesis/types.go`:
```go
package synthesis

import (
	"context"
	"time"
)

type Finding struct {
	Type       string   // cluster, drift, upstream_signal, staleness
	Severity   string   // info, warning, action_needed
	Title      string
	Evidence   []string
	Suggestion string
}

type Synthesizer interface {
	Name() string
	Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error)
}
```

`internal/synthesis/briefing.go`:
```go
package synthesis

import (
	"fmt"
	"strings"
)

func BuildBriefing(date string, findings []Finding) string {
	if len(findings) == 0 {
		return fmt.Sprintf("# [Briefing] Weekly — %s\n\nQuiet week. Not enough activity to produce a meaningful briefing.\n\n---\nGenerated by Repository Strategist.", date)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# [Briefing] Weekly — %s\n\n", date))

	sections := map[string][]Finding{
		"cluster":         nil,
		"drift":           nil,
		"staleness":       nil,
		"upstream_signal": nil,
	}
	for _, f := range findings {
		sections[f.Type] = append(sections[f.Type], f)
	}

	if clusters := sections["cluster"]; len(clusters) > 0 {
		b.WriteString("## Emerging Patterns\n\n")
		for _, f := range clusters {
			writeFinding(&b, f)
		}
		b.WriteString("\n")
	}

	if drifts := append(sections["drift"], sections["staleness"]...); len(drifts) > 0 {
		b.WriteString("## Decision Health\n\n")
		for _, f := range drifts {
			writeFinding(&b, f)
		}
		b.WriteString("\n")
	}

	if upstream := sections["upstream_signal"]; len(upstream) > 0 {
		b.WriteString("## Upstream Signals\n\n")
		for _, f := range upstream {
			writeFinding(&b, f)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\nGenerated by Repository Strategist. React with feedback or reply to discuss.\n")
	return b.String()
}

func writeFinding(b *strings.Builder, f Finding) {
	severity := ""
	if f.Severity == "action_needed" {
		severity = " [ACTION NEEDED]"
	}
	b.WriteString(fmt.Sprintf("**%s**%s\n\n", f.Title, severity))
	if f.Suggestion != "" {
		b.WriteString(f.Suggestion + "\n\n")
	}
	if len(f.Evidence) > 0 {
		b.WriteString("Evidence: " + strings.Join(f.Evidence, ", ") + "\n\n")
	}
}
```

`internal/synthesis/runner.go`:
```go
package synthesis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

type Runner struct {
	synthesizers []Synthesizer
	github       *gh.Client
	logger       *slog.Logger
}

func NewRunner(github *gh.Client, logger *slog.Logger, synthesizers ...Synthesizer) *Runner {
	return &Runner{synthesizers: synthesizers, github: github, logger: logger}
}

func (r *Runner) Run(ctx context.Context, installationID int64, repo, shadowRepo string, window time.Duration) error {
	var allFindings []Finding

	for _, s := range r.synthesizers {
		findings, err := s.Analyze(ctx, repo, window)
		if err != nil {
			r.logger.Error("synthesizer failed", "name", s.Name(), "error", err)
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	date := time.Now().Format("2006-01-02")
	briefing := BuildBriefing(date, allFindings)
	title := fmt.Sprintf("[Briefing] Weekly — %s", date)

	_, err := r.github.CreateIssue(ctx, installationID, shadowRepo, title, briefing)
	if err != nil {
		return fmt.Errorf("posting briefing: %w", err)
	}

	r.logger.Info("briefing posted", "repo", repo, "findings", len(allFindings))
	return nil
}
```

- [ ] **Step 3: Add `/synthesize` endpoint to server**

In `cmd/server/main.go`, after the `/ingest` handler, add:
```go
mux.HandleFunc("/synthesize", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" || !allowedRepos[repo] {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	// TODO: instantiate synthesizers and call runner.Run() once synthesizers are implemented (Tasks 12-14)
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok","findings":0}`)
})
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/synthesis/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/synthesis/types.go internal/synthesis/runner.go internal/synthesis/briefing.go internal/synthesis/briefing_test.go cmd/server/main.go
git commit -m "feat: add synthesis engine skeleton with briefing builder"
```

---

### Task 12: Issue Cluster Detection Synthesizer

**Files:**
- Create: `internal/synthesis/clusters.go`
- Create: `internal/synthesis/clusters_test.go`
- Modify: `internal/store/events.go` — add query for recent issue embeddings

- [ ] **Step 1: Add store method for recent issues with embeddings**

In `internal/store/events.go`, add:
```go
// RecentIssuesWithEmbeddings returns issues opened within the time window that have embeddings.
func (s *Store) RecentIssuesWithEmbeddings(ctx context.Context, repo string, since time.Time) ([]Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, number, title, summary, state, labels, embedding
		FROM issues
		WHERE repo = $1 AND created_at >= $2 AND embedding IS NOT NULL
		ORDER BY created_at DESC
	`, repo, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Issue
	for rows.Next() {
		var i Issue
		var vec pgvector.Vector
		if err := rows.Scan(&i.ID, &i.Repo, &i.Number, &i.Title, &i.Summary, &i.State, &i.Labels, &vec); err != nil {
			return nil, err
		}
		i.Embedding = vec.Slice()
		results = append(results, i)
	}
	return results, rows.Err()
}
```

Add `pgvector "github.com/pgvector/pgvector-go"` to the imports if not present.

- [ ] **Step 2: Write test for cluster detection logic**

```go
// internal/synthesis/clusters_test.go
package synthesis

import "testing"

func TestGroupClusters(t *testing.T) {
	// Simulate 5 issues: 3 similar (audio), 2 different
	issues := []clusterCandidate{
		{number: 1, title: "Audio broken after update", group: -1},
		{number: 2, title: "No sound in calls", group: -1},
		{number: 3, title: "Audio crackling on Linux", group: -1},
		{number: 4, title: "Dark mode not working", group: -1},
		{number: 5, title: "Button alignment off", group: -1},
	}

	// Simulate similarity matrix where issues 1-3 are similar
	similar := map[[2]int]bool{
		{0, 1}: true, {0, 2}: true, {1, 2}: true,
	}

	groups := groupBySimilarity(issues, func(i, j int) bool {
		return similar[[2]int{i, j}] || similar[[2]int{j, i}]
	})

	// Should find at least one cluster of size 3
	found := false
	for _, g := range groups {
		if len(g) >= 3 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find a cluster of 3+ issues")
	}
}
```

- [ ] **Step 3: Implement cluster detection synthesizer**

```go
// internal/synthesis/clusters.go
package synthesis

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

type ClusterSynthesizer struct {
	store     *store.Store
	threshold float64 // cosine distance threshold (lower = more similar)
	minSize   int
}

func NewClusterSynthesizer(s *store.Store) *ClusterSynthesizer {
	return &ClusterSynthesizer{store: s, threshold: 0.35, minSize: 3}
}

func (c *ClusterSynthesizer) Name() string { return "cluster_detection" }

func (c *ClusterSynthesizer) Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	since := time.Now().Add(-window)
	issues, err := c.store.RecentIssuesWithEmbeddings(ctx, repo, since)
	if err != nil {
		return nil, fmt.Errorf("fetching recent issues: %w", err)
	}
	if len(issues) < c.minSize {
		return nil, nil
	}

	candidates := make([]clusterCandidate, len(issues))
	for i, issue := range issues {
		candidates[i] = clusterCandidate{
			number:    issue.Number,
			title:     issue.Title,
			embedding: issue.Embedding,
			group:     -1,
		}
	}

	groups := groupBySimilarity(candidates, func(i, j int) bool {
		return cosineDistance(candidates[i].embedding, candidates[j].embedding) < c.threshold
	})

	var findings []Finding
	for _, group := range groups {
		if len(group) < c.minSize {
			continue
		}
		evidence := make([]string, len(group))
		titles := make([]string, len(group))
		for i, idx := range group {
			evidence[i] = fmt.Sprintf("#%d", candidates[idx].number)
			titles[i] = candidates[idx].title
		}

		// Check if there's ADR/roadmap coverage
		hasCoverage, _ := c.store.CountReferencesTo(ctx, repo, "issue", evidence[0])

		severity := "warning"
		suggestion := fmt.Sprintf("%d issues opened in the last %d days share a common theme. Topics: %s.",
			len(group), int(window.Hours()/24), strings.Join(titles[:min(3, len(titles))], "; "))
		if hasCoverage == 0 {
			suggestion += " No existing ADR or roadmap item covers this area. Consider investigating whether this warrants a roadmap task."
			severity = "action_needed"
		}

		findings = append(findings, Finding{
			Type:       "cluster",
			Severity:   severity,
			Title:      fmt.Sprintf("Issue cluster: %s (%d issues)", titles[0], len(group)),
			Evidence:   evidence,
			Suggestion: suggestion,
		})
	}

	return findings, nil
}

type clusterCandidate struct {
	number    int
	title     string
	embedding []float32
	group     int
}

func groupBySimilarity(candidates []clusterCandidate, isSimilar func(i, j int) bool) [][]int {
	n := len(candidates)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		pa, pb := find(a), find(b)
		if pa != pb {
			parent[pa] = pb
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if isSimilar(i, j) {
				union(i, j)
			}
		}
	}

	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	var result [][]int
	for _, g := range groups {
		result = append(result, g)
	}
	return result
}

func cosineDistance(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - (dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
```

Note: `"math"` is already in the import block above. Do NOT add a package-level `min` function — Go 1.21+ provides `min` as a builtin.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/synthesis/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/synthesis/clusters.go internal/synthesis/clusters_test.go internal/store/events.go
git commit -m "feat: add issue cluster detection synthesizer"
```

---

### Task 13: Decision Drift Detection Synthesizer

**Files:**
- Create: `internal/synthesis/drift.go`
- Create: `internal/synthesis/drift_test.go`

Detects ADR contradictions (PRs touching ADR-governed areas) and roadmap staleness (roadmap items with no recent activity).

- [ ] **Step 1: Write tests for roadmap staleness detection**

```go
// internal/synthesis/drift_test.go
package synthesis

import "testing"

func TestIsStale(t *testing.T) {
	tests := []struct {
		name          string
		activityCount int
		want          bool
	}{
		{"no activity is stale", 0, true},
		{"some activity not stale", 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStale(tt.activityCount); got != tt.want {
				t.Errorf("isStale(%d) = %v, want %v", tt.activityCount, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Implement drift detection synthesizer**

This synthesizer queries the event journal for `pr_merged` events in the window, checks if the changed files (from metadata) overlap with areas governed by ADRs (from the cross-reference index), and flags mismatches. It also queries roadmap documents and checks for recent activity via the event journal.

Implementation follows the same pattern as `clusters.go`: implements `Synthesizer` interface, queries store methods, returns `[]Finding`.

- [ ] **Step 3: Run tests and commit**

Run: `go test ./internal/synthesis/ -v`

```bash
git add internal/synthesis/drift.go internal/synthesis/drift_test.go
git commit -m "feat: add decision drift and roadmap staleness synthesizer"
```

---

### Task 14: Upstream Impact Analysis Synthesizer

**Files:**
- Create: `internal/synthesis/upstream.go`
- Create: `internal/synthesis/upstream_test.go`

Cross-references newly ingested upstream releases against ADRs and deferred roadmap items.

- [ ] **Step 1: Implement upstream impact synthesizer**

Queries recently ingested documents of type `upstream_release` (using `created_at` from documents table), embeds key phrases from the release, and compares against ADRs marked as "deferred" or "rejected" in metadata. When similarity is high, it produces opportunity or risk findings.

- [ ] **Step 2: Write tests and commit**

Follow same pattern as Tasks 12-13.

```bash
git add internal/synthesis/upstream.go internal/synthesis/upstream_test.go
git commit -m "feat: add upstream impact analysis synthesizer"
```

---

### Task 15: Wire Synthesizers into /synthesize Endpoint and Cron

**Files:**
- Modify: `cmd/server/main.go` — instantiate synthesizers and runner in `/synthesize` handler
- Create: `.github/workflows/synthesis.yml`

- [ ] **Step 1: Wire synthesizers into endpoint**

Replace the TODO in the `/synthesize` handler with actual synthesizer instantiation and runner invocation.

- [ ] **Step 2: Create synthesis cron workflow**

```yaml
# .github/workflows/synthesis.yml
name: Weekly Synthesis

on:
  schedule:
    - cron: '0 6 * * 1'  # Monday 06:00 UTC
  workflow_dispatch: {}

jobs:
  synthesize:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger synthesis
        env:
          INGEST_SECRET: ${{ secrets.INGEST_SECRET }}
          CLOUD_RUN_URL: ${{ secrets.CLOUD_RUN_URL }}
        run: |
          curl -sf -X POST "${CLOUD_RUN_URL}/synthesize?repo=IsmaelMartinez/teams-for-linux" \
            -H "Authorization: Bearer ${INGEST_SECRET}"
```

- [ ] **Step 3: Run full test suite and commit**

```bash
git add cmd/server/main.go .github/workflows/synthesis.yml
git commit -m "feat: wire synthesizers into /synthesize endpoint with weekly cron"
```

---

## Batch 3: Strategic Output (Month 3) — revised 2026-03-23

> **Revision note:** Tasks 16 and 18 have been delegated to [repo-butler](https://github.com/IsmaelMartinez/repo-butler), which already handles roadmap proposals (IDEATE+PROPOSE phases) and portfolio reporting (ASSESS+REPORT phases). This bot focuses on the intelligence layer: synthesis analysis, ADR lifecycle, and enriching `/report/trends` so repo-butler can consume the findings. See roadmap for the full rationale.

### ~~Task 16: Roadmap Task Generator~~ — DELEGATED to repo-butler

Repo-butler's IDEATE+PROPOSE phases already generate LLM-powered improvement ideas as GitHub issues with priority sorting and approval gates. Instead of duplicating this, Task 16a (below) enriches `/report/trends` so repo-butler can use cluster findings as IDEATE input context.

### Task 16a: Enrich /report/trends for repo-butler integration

**Files:**
- Modify: `internal/store/report.go` — add queries for recent cluster findings, drift signals, and upstream impacts
- Modify: `cmd/server/main.go` — extend `/report/trends` response with structured synthesis data

Expose synthesis findings as JSON so repo-butler's ASSESS phase can incorporate them into portfolio reports and IDEATE can use cluster data as context for generating better proposals.

- [ ] **Step 1-5: Implement, test, commit**

```bash
git commit -m "feat: enrich /report/trends with synthesis findings for repo-butler"
```

---

### Task 17: ADR Lifecycle Management

**Files:**
- Create: `internal/synthesis/adr.go`
- Create: `internal/synthesis/adr_test.go`

Two capabilities: ADR revision proposals (when drift is consistent across multiple PRs) and ADR gap detection (when issue clusters exist in areas with no ADR).

- [ ] **Step 1-5: Implement, test, commit**

```bash
git commit -m "feat: add ADR revision proposals and gap detection"
```

---

### ~~Task 18: State of the Project Monthly Briefing~~ — DELEGATED to repo-butler

Repo-butler's ASSESS+REPORT phases already produce per-repo HTML dashboards with PR velocity, issue trends, release cadence, and weekly trend computation deployed to GitHub Pages. Monthly briefings would duplicate this. Instead, Task 16a enriches `/report/trends` so repo-butler can incorporate synthesis findings (clusters, drift, upstream signals) into its richer portfolio reports.

---

### Task 19: Dashboard — Synthesis Metrics

**Files:**
- Modify: `internal/store/report.go` — add synthesis stats (briefings posted, findings by type, proposals accepted/rejected) and storage usage metrics (total rows per table, estimated size)
- Modify: `cmd/server/template.html` — add synthesis section and storage health card to dashboard

- [ ] **Step 1-5: Implement, test, commit**

```bash
git commit -m "feat: add synthesis metrics to dashboard"
```

---

### Task 20: Multi-Repo Hardening and Documentation

**Files:**
- Modify: `README.md` — strategist framing, getting-started guide, butler.json schema docs
- Create: `internal/config/validate.go` — config validation with helpful error messages
- Create: `internal/config/validate_test.go`

- [ ] **Step 1: Write config validation**

Validate that `shadow_repo` is set when `synthesis` is enabled, that `max_daily_llm_calls` is within bounds, and that `doc_paths` are valid glob patterns.

- [ ] **Step 2: Update README**

Add sections: "What is the Repository Strategist?", "Getting Started" (install App → add butler.json → seed docs → wait for briefing), "butler.json Reference", "Architecture".

- [ ] **Step 3: Run full test suite**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add README.md internal/config/validate.go internal/config/validate_test.go
git commit -m "feat: add config validation and strategist documentation"
```

---

### Task 21: LLM Usage Tracking

**Files:**
- Create: `internal/llm/budget.go`
- Create: `internal/llm/budget_test.go`

In-memory daily counter that tracks Gemini API calls across triage and synthesis. Logs warnings at 80% of the 250/day limit. The synthesis runner checks this before making LLM calls and skips LLM-dependent synthesizers when the budget is exhausted.

- [ ] **Step 1-5: Implement, test, commit**

```bash
git commit -m "feat: add LLM daily usage budget tracker"
```

---

## Summary

| Batch | Tasks | Key Deliverables |
|-------|-------|------------------|
| 1 (Month 1) | 1-10 | butler.json config, event journal, auto-ingest, cross-reference index |
| 2 (Month 2) | 11-15 | Synthesis engine, cluster detection, drift detection, upstream impact, weekly briefings |
| 3 (Month 3, revised) | 16a, 17, 19-21 | Enrich /report/trends for repo-butler, ADR lifecycle, dashboard metrics, multi-repo docs, LLM budget (done) |

Tasks 16 (roadmap proposals) and 18 (monthly briefings) delegated to repo-butler (2026-03-23). This bot provides the intelligence layer via API; repo-butler handles presentation and action.

Each batch produces independently testable, deployable code. Month 1 can run in production with no visible change to users (events are journaled silently). Month 2 starts producing weekly briefings in the shadow repo. Month 3 enriches the API surface for repo-butler integration and adds ADR lifecycle management.
