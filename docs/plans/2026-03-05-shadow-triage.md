# Shadow Triage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Route all triage phase results through the shadow repo for maintainer review before posting publicly, replacing the DB-only silent mode with the same shadow-repo staging pattern used by the Enhancement Researcher agent.

**Architecture:** When an issue is opened, run all phases, create a mirror issue in the shadow repo (`[Triage] #<num>: <title>`), post the triage comment there, and store the mapping in a new `triage_sessions` table. A maintainer replies `lgtm` to promote to the public issue or `reject` to discard. When no shadow repo is configured, behavior falls back to posting directly (existing path). The `triage_results` table and `SILENT_MODE` are removed entirely.

**Tech Stack:** Go 1.26, pgx/v5, GitHub App API (existing `gh.Client`), existing agent orchestrator signals.

---

### Task 1: Add database migrations

**Files:**
- Create: `migrations/007_triage_sessions.sql`
- Create: `migrations/008_drop_triage_results.sql`

**Step 1: Write `007_triage_sessions.sql`**

```sql
CREATE TABLE IF NOT EXISTS triage_sessions (
    id                  BIGSERIAL PRIMARY KEY,
    repo                TEXT NOT NULL,
    issue_number        INTEGER NOT NULL,
    shadow_repo         TEXT NOT NULL,
    shadow_issue_number INTEGER NOT NULL,
    triage_comment      TEXT NOT NULL,
    phases_run          TEXT[] NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, issue_number)
);
CREATE INDEX IF NOT EXISTS idx_triage_sessions_shadow ON triage_sessions (shadow_repo, shadow_issue_number);
```

**Step 2: Write `008_drop_triage_results.sql`**

```sql
DROP TABLE IF EXISTS triage_results;
```

**Step 3: Apply migrations manually (requires DATABASE_URL)**

```bash
psql "$DATABASE_URL" -f migrations/007_triage_sessions.sql
psql "$DATABASE_URL" -f migrations/008_drop_triage_results.sql
```

Expected: `CREATE TABLE`, `CREATE INDEX`, `DROP TABLE`

**Step 4: Commit**

```bash
git add migrations/007_triage_sessions.sql migrations/008_drop_triage_results.sql
git commit -m "feat: add triage_sessions migration and drop triage_results"
```

---

### Task 2: Add triage session store methods

**Files:**
- Create: `internal/store/triage_session.go`
- Modify: `internal/store/models.go` — add `TriageSession` struct, remove `TriageResultRecord`
- Modify: `internal/store/postgres.go` — remove `HasTriageResult`, `RecordTriageResult`

**Step 1: Write failing tests**

Create `internal/store/triage_session_test.go`:

```go
package store_test

import (
    "testing"
    "github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestTriageSessionRoundtrip(t *testing.T) {
    // Compile-time check: methods exist on *Store
    var s *store.Store
    var _ = s.CreateTriageSession
    var _ = s.GetTriageSessionByShadow
    var _ = s.HasTriageSession
}
```

Run: `go test ./internal/store/... -run TestTriageSessionRoundtrip`
Expected: FAIL — methods don't exist yet.

**Step 2: Add `TriageSession` to `internal/store/models.go`**

Add after the existing model structs:

```go
// TriageSession tracks a shadow issue created for triage review.
type TriageSession struct {
    ID                int64
    Repo              string
    IssueNumber       int
    ShadowRepo        string
    ShadowIssueNumber int
    TriageComment     string
    PhasesRun         []string
}
```

Also remove the `TriageResultRecord` struct from `models.go` (search for it and delete).

**Step 3: Create `internal/store/triage_session.go`**

```go
package store

import "context"

// CreateTriageSession inserts a new triage session record.
func (s *Store) CreateTriageSession(ctx context.Context, ts TriageSession) error {
    _, err := s.pool.Exec(ctx, `
        INSERT INTO triage_sessions (repo, issue_number, shadow_repo, shadow_issue_number, triage_comment, phases_run)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (repo, issue_number) DO UPDATE
            SET shadow_repo = EXCLUDED.shadow_repo,
                shadow_issue_number = EXCLUDED.shadow_issue_number,
                triage_comment = EXCLUDED.triage_comment,
                phases_run = EXCLUDED.phases_run
    `, ts.Repo, ts.IssueNumber, ts.ShadowRepo, ts.ShadowIssueNumber, ts.TriageComment, ts.PhasesRun)
    return err
}

// GetTriageSessionByShadow returns the triage session for a given shadow issue, or nil if not found.
func (s *Store) GetTriageSessionByShadow(ctx context.Context, shadowRepo string, shadowIssueNumber int) (*TriageSession, error) {
    var ts TriageSession
    err := s.pool.QueryRow(ctx, `
        SELECT id, repo, issue_number, shadow_repo, shadow_issue_number, triage_comment, phases_run
        FROM triage_sessions WHERE shadow_repo = $1 AND shadow_issue_number = $2
    `, shadowRepo, shadowIssueNumber).Scan(
        &ts.ID, &ts.Repo, &ts.IssueNumber, &ts.ShadowRepo, &ts.ShadowIssueNumber, &ts.TriageComment, &ts.PhasesRun,
    )
    if err != nil {
        if isNotFound(err) {
            return nil, nil
        }
        return nil, err
    }
    return &ts, nil
}

// HasTriageSession returns true if a triage session already exists for the given issue.
func (s *Store) HasTriageSession(ctx context.Context, repo string, issueNumber int) (bool, error) {
    var count int
    err := s.pool.QueryRow(ctx, `
        SELECT COUNT(*) FROM triage_sessions WHERE repo = $1 AND issue_number = $2
    `, repo, issueNumber).Scan(&count)
    return count > 0, err
}
```

Note: `isNotFound` is a helper that already exists in `postgres.go` — check for `pgx.ErrNoRows`. If it doesn't exist, add: `func isNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }` and import `"github.com/jackc/pgx/v5"` and `"errors"`.

**Step 4: Remove old methods from `internal/store/postgres.go`**

Delete `HasTriageResult` and `RecordTriageResult` functions. Search for them with:
```bash
grep -n "HasTriageResult\|RecordTriageResult" internal/store/postgres.go
```

**Step 5: Run tests**

```bash
go build ./...
go test ./internal/store/...
```

Expected: PASS (compile-time check passes, no DB needed for the test we wrote)

**Step 6: Commit**

```bash
git add internal/store/triage_session.go internal/store/triage_session_test.go internal/store/models.go internal/store/postgres.go
git commit -m "feat: add triage session store, remove triage_results store methods"
```

---

### Task 3: Add `CloseIssue` to GitHub client

**Files:**
- Modify: `internal/github/client.go`

**Step 1: Check existing client methods**

```bash
grep -n "^func (c \*Client)" internal/github/client.go
```

**Step 2: Add `CloseIssue` method**

Find the end of `client.go` and append:

```go
// CloseIssue closes a GitHub issue by setting its state to "closed".
func (c *Client) CloseIssue(ctx context.Context, installationID int64, repo string, issueNumber int) error {
    client, err := c.installationClient(installationID)
    if err != nil {
        return fmt.Errorf("installation client: %w", err)
    }

    payload := map[string]string{"state": "closed"}
    raw, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal payload: %w", err)
    }

    url := fmt.Sprintf("%s/repos/%s/issues/%d", c.baseURL, repo, issueNumber)
    req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
    if err != nil {
        return fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/vnd.github+json")

    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("send request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
    }
    return nil
}
```

**Step 3: Build**

```bash
go build ./internal/github/...
```

Expected: success

**Step 4: Commit**

```bash
git add internal/github/client.go
git commit -m "feat: add CloseIssue to GitHub client"
```

---

### Task 4: Update `handleOpened` to post to shadow repo

**Files:**
- Modify: `internal/webhook/handler.go`

This is the core change. In `handleOpened`, the current `switch { case h.silentMode: ... case body != "": ... }` block is replaced with: if shadow repo configured → post to shadow and store session; else → post directly to public.

**Step 1: Update `handleOpened` in `handler.go`**

Replace the block starting at `switch {` (around line 299) through the closing `}` of the switch (around line 332) with:

```go
if shadowRepo, ok := h.shadowRepos[repo]; ok && body != "" {
    // Post to shadow repo for review
    shadowTitle := fmt.Sprintf("[Triage] #%d: %s", issue.Number, issue.Title)
    shadowBody := gh.FormatShadowIssueBody(repo, issue.Number, issue.Title, issue.Body)
    shadowNumber, err := h.github.CreateIssue(ctx, installationID, shadowRepo, shadowTitle, shadowBody)
    if err != nil {
        issueLog.Error("creating shadow triage issue", "error", err)
    } else {
        instructions := "\n\n---\n\nReply `lgtm` to post this comment publicly, or `reject` to discard."
        _, err = h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, body+instructions)
        if err != nil {
            issueLog.Error("posting triage comment on shadow issue", "error", err)
        }
        if err := h.store.CreateTriageSession(ctx, store.TriageSession{
            Repo:              repo,
            IssueNumber:       issue.Number,
            ShadowRepo:        shadowRepo,
            ShadowIssueNumber: shadowNumber,
            TriageComment:     body,
            PhasesRun:         phasesRun,
        }); err != nil {
            issueLog.Error("recording triage session", "error", err)
        }
        issueLog.Info("triage comment posted to shadow repo", "shadowRepo", shadowRepo, "shadowIssue", shadowNumber)
    }
} else if body != "" {
    commentID, err := h.github.CreateComment(ctx, installationID, repo, issue.Number, body)
    if err != nil {
        issueLog.Error("posting comment", "error", err)
    } else {
        if err := h.store.RecordBotComment(ctx, store.BotComment{
            Repo:        repo,
            IssueNumber: issue.Number,
            CommentID:   commentID,
            PhasesRun:   phasesRun,
        }); err != nil {
            issueLog.Error("recording bot comment", "error", err)
        }
        issueLog.Info("comment posted", "phases", phasesRun)
    }
} else {
    issueLog.Info("nothing to report in triage comment")
}
```

**Step 2: Update the "already processed" check in `handleOpened`**

The current check (around line 209-228) checks `HasBotCommented` and `HasTriageResult` (with `silentMode`). Replace it with:

```go
commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
if err != nil {
    issueLog.Error("checking bot comment", "error", err)
    return
}
if commented {
    issueLog.Info("bot already commented")
    return
}
triaged, err := h.store.HasTriageSession(ctx, repo, issue.Number)
if err != nil {
    issueLog.Error("checking triage session", "error", err)
    return
}
if triaged {
    issueLog.Info("already triaged via shadow repo")
    return
}
```

**Step 3: Remove `silentMode` field and related code from `Handler`**

- Remove `silentMode bool` from the `Handler` struct
- Remove `silentMode bool` parameter from `New(...)` and the assignment `silentMode: silentMode`
- Delete `buildPhaseDetails` function (no longer needed)

**Step 4: Build**

```bash
go build ./internal/webhook/...
```

Fix any compilation errors (likely missing imports or references to removed fields).

**Step 5: Run tests**

```bash
go test ./internal/webhook/...
```

**Step 6: Commit**

```bash
git add internal/webhook/handler.go
git commit -m "feat: post triage to shadow repo instead of silent DB storage"
```

---

### Task 5: Handle triage signals in `processCommentEvent`

**Files:**
- Modify: `internal/webhook/handler.go` — extend `processCommentEvent`

When a comment is posted on a shadow issue, it might be for an agent session OR a triage session. Currently `HandleComment` only checks agent sessions. We need to also check triage sessions.

**Step 1: Add `handleTriageComment` method to `handler.go`**

```go
func (h *Handler) handleTriageComment(ctx context.Context, installationID int64, shadowRepo string, shadowIssueNumber int, commentBody string, commentUser string) (bool, error) {
    ts, err := h.store.GetTriageSessionByShadow(ctx, shadowRepo, shadowIssueNumber)
    if err != nil {
        return false, err
    }
    if ts == nil {
        return false, nil // not a triage session
    }

    log := h.logger.With("repo", ts.Repo, "issue", ts.IssueNumber, "shadowRepo", shadowRepo, "shadowIssue", shadowIssueNumber)
    signal := agent.ParseApprovalSignal(commentBody)

    switch signal {
    case agent.SignalApproved:
        commentID, err := h.github.CreateComment(ctx, installationID, ts.Repo, ts.IssueNumber, ts.TriageComment)
        if err != nil {
            return true, fmt.Errorf("post triage comment publicly: %w", err)
        }
        if err := h.store.RecordBotComment(ctx, store.BotComment{
            Repo:        ts.Repo,
            IssueNumber: ts.IssueNumber,
            CommentID:   commentID,
            PhasesRun:   ts.PhasesRun,
        }); err != nil {
            log.Error("recording bot comment after promotion", "error", err)
        }
        _ = h.github.CloseIssue(ctx, installationID, shadowRepo, shadowIssueNumber)
        log.Info("triage comment promoted to public issue")

    case agent.SignalReject:
        _ = h.github.CloseIssue(ctx, installationID, shadowRepo, shadowIssueNumber)
        log.Info("triage session rejected")

    default:
        log.Info("ignoring non-signal comment on triage shadow issue")
    }

    return true, nil
}
```

**Step 2: Update `processCommentEvent` to call `handleTriageComment` first**

Replace the body of `processCommentEvent`:

```go
func (h *Handler) processCommentEvent(ctx context.Context, event gh.IssueCommentEvent) {
    repo := event.Repo.FullName
    commentUser := event.Comment.User.Login
    commentBody := event.Comment.Body
    issueNumber := event.Issue.Number
    installationID := event.Installation.ID

    if event.Comment.User.Type == "Bot" {
        return
    }

    log := h.logger.With("repo", repo, "issue", issueNumber, "commentUser", commentUser)
    log.Info("processing comment event")

    // Check triage session first
    handled, err := h.handleTriageComment(ctx, installationID, repo, issueNumber, commentBody, commentUser)
    if err != nil {
        log.Error("handling triage comment", "error", err)
    }
    if handled {
        return
    }

    // Fall through to agent session handler
    if err := h.agentHandler.HandleComment(ctx, installationID, repo, issueNumber, commentBody, commentUser); err != nil {
        log.Error("handling agent comment", "error", err)
    }
}
```

**Step 3: Build**

```bash
go build ./internal/webhook/...
```

**Step 4: Run tests**

```bash
go test ./internal/webhook/...
```

**Step 5: Commit**

```bash
git add internal/webhook/handler.go
git commit -m "feat: handle lgtm/reject signals on shadow triage issues"
```

---

### Task 6: Remove `SILENT_MODE` from `cmd/server/main.go`

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: Find and remove SILENT_MODE**

```bash
grep -n "SILENT_MODE\|silentMode\|silent_mode" cmd/server/main.go
```

Remove the `os.Getenv("SILENT_MODE")` block and the `silentMode` variable. Update the `webhook.New(...)` call to remove the `silentMode` argument (Task 4 removed the parameter).

**Step 2: Build**

```bash
go build ./cmd/server/...
```

**Step 3: Run all tests**

```bash
go test ./...
```

**Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "chore: remove SILENT_MODE env var"
```

---

### Task 7: Update dashboard — remove Silent Drafts

**Files:**
- Modify: `internal/store/report.go` — remove `TotalDrafts`, `RecentDrafts`, and related queries
- Modify: `internal/store/models.go` — remove `RecentDraft` struct (if present)
- Modify: `cmd/dashboard/template.html` — remove the `#drafts-section`

**Step 1: Update `DashboardStats` in `report.go`**

Remove `TotalDrafts int` and `RecentDrafts []RecentDraft` from the struct. Remove the two query blocks that scan from `triage_results`. Remove the `RecentDraft` type if it's defined in `report.go` (check — it may be in `models.go`).

**Step 2: Update `template.html`**

Remove the entire `<section id="drafts-section" ...>` block (lines 55-63) and the corresponding JS block that populates it (the `var drafts = ...` block).

Also remove the `{ label: 'Silent Drafts', value: stats.total_drafts || 0 }` card entry from the `cards` array in the JS.

**Step 3: Build and test**

```bash
go build ./...
go test ./...
```

**Step 4: Commit**

```bash
git add internal/store/report.go cmd/dashboard/template.html
git commit -m "chore: remove silent drafts from dashboard"
```

---

### Task 8: Update CLAUDE.md and run linter

**Files:**
- Modify: `CLAUDE.md` — remove `SILENT_MODE` from environment variable docs, update architecture description

**Step 1: Update env vars section in CLAUDE.md**

Remove the `SILENT_MODE` entry. Update the architecture description to say "posts triage results to shadow repo for review" instead of "stores results in triage_results".

**Step 2: Run linter**

```bash
golangci-lint run ./...
```

Fix any linter errors.

**Step 3: Run full test suite**

```bash
go test ./...
go vet ./...
```

**Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for shadow triage flow"
```

---

## Execution Options

Plan saved to `docs/plans/2026-03-05-shadow-triage.md`.

**1. Subagent-Driven (this session)** — fresh subagent per task, code review between tasks, fast iteration.

**2. Parallel Session (separate)** — open a new session in this worktree and use `superpowers:executing-plans`.

Which approach?
