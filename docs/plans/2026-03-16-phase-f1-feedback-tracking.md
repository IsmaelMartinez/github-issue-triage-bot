# Phase F1: Feedback Tracking

Date: 2026-03-16
Status: Future (not started)

## Summary

Phase F1 closes the feedback loop on the bot's triage output by tracking two signal types: whether users edit their issue to fill in sections that Phase 1 flagged as missing (quantitative), and whether users reply mentioning @ismael-triage-bot with feedback (qualitative). Both webhook events already arrive at the bot but are currently ignored. The signals are stored in a new `feedback_signals` table and surfaced on the dashboard as Phase 1 fill rate and a recent feedback feed.

## Spike Results

These facts were verified against the live codebase:

The `issues.edited` webhook payload includes `changes.body.from` containing the previous body text, so detecting what changed costs zero API calls. The bot currently ignores all actions except `opened`, `closed`, and `reopened` in the `processEvent` switch in `internal/webhook/handler.go` (line 266). The `IssueEvent` struct in `internal/github/client.go` does not yet have a `Changes` field, so one needs to be added.

Phase 1 output is a `Phase1Result` struct (`internal/phases/types.go`) containing `MissingItems []MissingItem` where each item has a `Label` (e.g. "Reproduction steps", "Debug console output", "Expected behavior", "PWA reproducibility") and a `Detail` string. The same Phase 1 function can be re-run against both the old and new body to compute a diff.

The triage session table (`triage_sessions`) stores `repo`, `issue_number`, `phases_run`, and `triage_comment`, but not the issue body at triage time. We need the original body to detect edits in case the `changes.body.from` field ever arrives stale, and more importantly to re-run Phase 1 against the original to know which items were flagged. However, since we can re-run `Phase1(oldBody)` on the `changes.body.from` value from the webhook, we do not strictly need to store the body snapshot. The simpler design is to rely on the webhook payload alone.

The `issue_comment` webhook is already processed by `processCommentEvent` in `handler.go`. It checks for `/retriage`, then for triage signals (lgtm/reject on shadow issues), then falls through to the agent handler. Comments on the source repo that are not `/retriage` and not on a shadow issue currently fall through without being processed. This is where @mention detection will hook in.

The latest migration is `009_triage_session_closed_at.sql`. The next migration will be `010`.

The bot username used in the footer is `@ismael-triage-bot` (hardcoded in `internal/comment/builder.go` lines 127-131).

## Data Flow

### Edit Detection Flow

```
GitHub issues.edited webhook
    |
    v
handler.go: ServeHTTP (event type "issues")
    |
    v
processEvent: action == "edited"
    |
    v
handleEdited(ctx, installationID, repo, issue, changes)
    |
    +-- Look up bot_comments or triage_sessions for (repo, issue_number)
    |   (if none found, this issue was never triaged -- ignore)
    |
    +-- Re-run Phase1(changes.Body.From) to get original missing items
    +-- Re-run Phase1(issue.Body) to get current missing items
    |
    +-- Compute filled items = items in original but not in current
    |
    +-- If any items were filled:
    |       Store feedback_signal (signal_type: 'issue_edit_filled',
    |                              details: {filled_items: [...], total_flagged: N})
    |
    +-- Also call upsertIssue to update the issue embedding with the new body
```

### @mention Detection Flow

```
GitHub issue_comment webhook (action: "created")
    |
    v
processCommentEvent (existing flow)
    |
    +-- Skip bot comments (existing)
    +-- Check /retriage (existing)
    +-- Check triage signals on shadow issues (existing)
    +-- Check agent session signals (existing)
    |
    +-- NEW: if comment body contains "@ismael-triage-bot"
    |       and issue has a bot_comment or triage_session:
    |
    |       Store feedback_signal (signal_type: 'user_mention',
    |                              details: {comment_id: N, body: "...", user: "..."})
```

## Migration Schema

### Migration 010: feedback_signals table

```sql
CREATE TABLE IF NOT EXISTS feedback_signals (
    id              BIGSERIAL PRIMARY KEY,
    repo            TEXT NOT NULL,
    issue_number    INTEGER NOT NULL,
    signal_type     TEXT NOT NULL,  -- 'issue_edit_filled', 'user_mention'
    details         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_signals_repo
    ON feedback_signals (repo);
CREATE INDEX IF NOT EXISTS idx_feedback_signals_repo_issue
    ON feedback_signals (repo, issue_number);
CREATE INDEX IF NOT EXISTS idx_feedback_signals_type
    ON feedback_signals (signal_type);
```

No unique constraint on (repo, issue_number, signal_type) because a user might edit their issue multiple times, and multiple users might @mention the bot on the same issue. Each event produces a separate row.

No body snapshot column on `triage_sessions` is needed. The `changes.body.from` field in the webhook payload gives us the pre-edit body, and we re-run Phase 1 against both old and new to compute the diff. This avoids a schema migration on an existing table.

## Implementation Steps

### Step 1: Add Changes field to IssueEvent

File: `internal/github/client.go`

The `IssueEvent` struct needs a new `Changes` field to capture the `changes` payload that GitHub sends on `issues.edited` events. Add:

```go
type IssueEvent struct {
    Action       string           `json:"action"`
    Issue        IssueDetail      `json:"issue"`
    Changes      *IssueChanges    `json:"changes,omitempty"`
    Repo         RepoDetail       `json:"repository"`
    Installation InstallationInfo `json:"installation"`
}

type IssueChanges struct {
    Body *IssueChangeField `json:"body,omitempty"`
}

type IssueChangeField struct {
    From string `json:"from"`
}
```

The `Changes` pointer is nil when the action is not `edited`, so existing callers are unaffected.

### Step 2: Add FeedbackSignal model and store methods

File: `internal/store/models.go` -- add the model:

```go
type FeedbackSignal struct {
    ID          int64
    Repo        string
    IssueNumber int
    SignalType  string
    Details     map[string]any
    CreatedAt   time.Time
}
```

File: `internal/store/feedback.go` (new file) -- add store methods:

```go
func (s *Store) RecordFeedbackSignal(ctx context.Context, sig FeedbackSignal) error
```

This method inserts a row into `feedback_signals`, marshaling `Details` as JSONB. It follows the same pattern as `CreateAuditEntry` in `internal/store/agent.go`.

```go
func (s *Store) GetFeedbackStats(ctx context.Context, repo string) (*FeedbackStats, error)
```

This method returns aggregated stats for the dashboard. The `FeedbackStats` struct:

```go
type FeedbackStats struct {
    TotalEditSignals   int                `json:"total_edit_signals"`
    TotalMentions      int                `json:"total_mentions"`
    FillRate           *float64           `json:"fill_rate"`
    RecentFeedback     []RecentFeedback   `json:"recent_feedback"`
}

type RecentFeedback struct {
    Repo        string `json:"repo"`
    IssueNumber int    `json:"issue_number"`
    SignalType  string `json:"signal_type"`
    CreatedAt   string `json:"created_at"`
}
```

`FillRate` is computed as: (count of `issue_edit_filled` signals) / (count of triage sessions that ran phase1 and had missing items). This requires a join or a subquery against `triage_sessions` where `'phase1' = ANY(phases_run)`. Since we don't store whether Phase 1 found missing items in the triage session record, we approximate with all triage sessions that ran phase1 (every session runs phase1, so this is effectively the total session count). Alternatively, we count the total issues that received a promoted bot comment containing "Missing information" in the triage_comment -- but that couples to comment text. The simplest approach: fill rate = `issue_edit_filled` signal count / total promoted triage sessions (those with matching bot_comments). This gives the fraction of promoted triage comments where users acted on the missing info request.

### Step 3: Handle `issues.edited` in webhook handler

File: `internal/webhook/handler.go`

In `processEvent`, add a case for the `"edited"` action:

```go
case "edited":
    h.handleEdited(ctx, installationID, repo, issue, event.Changes)
```

New method `handleEdited(ctx context.Context, installationID int64, repo string, issue gh.IssueDetail, changes *gh.IssueChanges)`:

The method does the following, in order:

First, it calls `upsertIssue` to update the issue embedding with the new body (this is useful regardless of feedback tracking, since it keeps vector search current).

Second, it checks whether a body change occurred. If `changes` is nil or `changes.Body` is nil, there is no body edit to analyze (the edit might have been a title or label change), so it returns early.

Third, it checks whether this issue was triaged. It calls `h.store.HasBotCommented(ctx, repo, issue.Number)` and `h.store.HasTriageSession(ctx, repo, issue.Number)`. If neither returns true, the issue was never triaged, so there is no feedback to track.

Fourth, it runs Phase 1 against both bodies:

```go
oldResult := phases.Phase1(changes.Body.From)
newResult := phases.Phase1(issue.Body)
```

Fifth, it computes which items were filled. A "filled" item is one whose `Label` appears in `oldResult.MissingItems` but not in `newResult.MissingItems`. This is a simple set difference on the `Label` field.

Sixth, if any items were filled, it stores a feedback signal:

```go
h.store.RecordFeedbackSignal(ctx, store.FeedbackSignal{
    Repo:        repo,
    IssueNumber: issue.Number,
    SignalType:  "issue_edit_filled",
    Details: map[string]any{
        "filled_items":  filledLabels,
        "total_flagged": len(oldResult.MissingItems),
        "remaining":     len(newResult.MissingItems),
    },
})
```

The method logs at Info level when a fill signal is recorded, and at Debug level when an edit is processed but no fill is detected.

### Step 4: Handle @mention feedback in comment handler

File: `internal/webhook/handler.go`

In `processCommentEvent`, after the agent handler fallthrough (line 216), add @mention detection:

```go
h.checkMentionFeedback(ctx, repo, issueNumber, event.Comment)
```

New method `checkMentionFeedback(ctx context.Context, repo string, issueNumber int, comment gh.CommentDetail)`:

First, it checks whether the comment body contains the bot mention string `@ismael-triage-bot` (case-sensitive, matching the footer text). If not, return.

Second, it checks whether this issue was triaged (same `HasBotCommented` / `HasTriageSession` check as edit detection). If not, return.

Third, it stores a feedback signal:

```go
h.store.RecordFeedbackSignal(ctx, store.FeedbackSignal{
    Repo:        repo,
    IssueNumber: issue.Number,
    SignalType:  "user_mention",
    Details: map[string]any{
        "comment_id": comment.ID,
        "body":       truncate(comment.Body, 500),
        "user":       comment.User.Login,
    },
})
```

The body is truncated to 500 characters to avoid storing large payloads. The truncation helper reuses the UTF-8-safe truncation pattern from `sanitizeBody`.

The bot mention string should be a package-level constant (`botMentionHandle = "@ismael-triage-bot"`) to keep it in one place. If the bot username changes, only this constant and the footer in `builder.go` need updating.

### Step 5: Add feedback stats to dashboard

File: `internal/store/report.go`

Add a `FeedbackStats` field to `DashboardStats`:

```go
FeedbackStats *FeedbackStats `json:"feedback_stats"`
```

In `GetDashboardStats`, call `s.getFeedbackStats(ctx, repo)` and assign the result. The implementation follows the pattern of `getTriageStats` -- a private helper that runs a few queries and returns a struct.

The queries:

```sql
-- Count by signal type
SELECT signal_type, COUNT(*) FROM feedback_signals
WHERE repo = $1 GROUP BY signal_type

-- Recent 10 feedback signals
SELECT repo, issue_number, signal_type, created_at
FROM feedback_signals WHERE repo = $1
ORDER BY created_at DESC LIMIT 10
```

File: `cmd/server/template.html`

Add a "Feedback" section to the Triage view (since edit detection relates to Phase 1, which is part of bug triage). The section should contain:

A health card for "Edit Fill Rate" showing the percentage of triaged issues where users filled in missing sections after the bot commented. This appears alongside the existing triage cards in `#triage-cards`.

A small panel titled "Recent Feedback" showing the last 10 feedback signals, with columns: Issue (link), Type (tag: "edit" or "mention"), and Date. This goes after the existing "Recent Sessions" panel in the triage view.

The JavaScript data binding follows the existing pattern: read `stats.feedback_stats`, create cards with `addCards()`, fill table with `fillTbl()`.

### Step 6: Create migration file

File: `migrations/010_feedback_signals.sql`

Contains the CREATE TABLE and CREATE INDEX statements from the schema section above.

## Edit Detection: Detailed Algorithm

The core logic for comparing old vs new body against Phase 1 results is intentionally simple and reuses Phase 1 directly rather than doing custom diff parsing.

```
function computeFilledItems(oldBody, newBody):
    oldResult = Phase1(oldBody)
    newResult = Phase1(newBody)

    oldMissing = set of Label values from oldResult.MissingItems
    newMissing = set of Label values from newResult.MissingItems

    filled = oldMissing - newMissing    // set difference

    return filled
```

This works because Phase 1 is pure string parsing with no side effects and no external dependencies. Calling it twice per edit event is cheap (sub-millisecond). The labels are stable strings defined in `phase1.go`: "Reproduction steps", "Debug console output", "Expected behavior", "PWA reproducibility".

Edge cases to handle:

If the user adds a completely new section header that Phase 1 doesn't recognize, the missing count won't change -- this is correct behavior since we're only tracking the specific items Phase 1 flagged.

If the user partially fills in a section (e.g. adds some text but it still matches the default template check), Phase 1 will still flag it as missing. This is also correct -- the user hasn't meaningfully filled in the section.

If the issue body had no template headers at all (free-form text), Phase 1 returns all 4 items as missing. An edit that adds template sections and fills them in will correctly detect the fill. An edit that just modifies the free-form text without adding template headers will show no change (still 4 missing), so no signal is recorded.

If Phase 1 was not relevant (the issue is an enhancement, not a bug), Phase 1 still runs and returns results. However, the bot comment only includes missing info for bugs. For enhancements, Phase 1 may flag missing items but they are not shown to the user, so edit signals on enhancements would be misleading. To handle this, the `handleEdited` method should check whether the issue has the "bug" label before recording an edit fill signal. The label check can use the existing `hasLabel` helper with `issue.Labels`.

## @mention Detection: Details

The mention check is a simple `strings.Contains(commentBody, "@ismael-triage-bot")` call. This is intentionally case-sensitive because GitHub usernames are case-sensitive in mentions and the footer uses the exact string `@ismael-triage-bot`.

The check only runs on the source repo (not shadow repos), because the bot footer is only present on comments posted to the public issue. Shadow repo comments go through the triage/agent signal flow instead.

To avoid storing the mention if the commenter is the bot itself (e.g. if the bot's own comment contains its handle), the existing bot skip check at the top of `processCommentEvent` (`event.Comment.User.Type == "Bot"`) already handles this.

The mention feedback is purely passive storage for now -- no automated response to the user. Future work could parse sentiment or categorize the feedback, but that's out of scope for F1.

## Dashboard Integration

The Triage view gains two new elements:

A "Phase 1 Fill Rate" health card is added to the `#triage-cards` grid. The value is computed client-side from `stats.feedback_stats.fill_rate`. The card uses `hc-green` when fill rate >= 50%, `hc-yellow` when >= 25%, `hc-red` otherwise. A null fill rate (no data) shows "N/A" with `hc-neutral`.

A "Recent Feedback" panel is added below the "Recent Sessions" panel. It contains a table with columns Issue, Type, and Date. The Type column uses tag styling: `tag-promoted` for "edit" signals (positive outcome), `tag-default` for "mention" signals.

The sidebar nav does not need a new view -- feedback is a facet of the triage flow, not a separate concern. If the feedback data grows rich enough to warrant its own view later, that's a future change.

## Testing Approach

### Edit detection (table-driven tests)

File: `internal/webhook/handler_test.go` or a new `internal/webhook/feedback_test.go`

The `computeFilledItems` logic should be extracted into a pure function (e.g. `computeFilledSections(oldBody, newBody string) []string`) that is testable independently of the handler. Table-driven tests:

```
test: "user fills all missing sections"
  oldBody: template with all default placeholders
  newBody: template with real content in all sections
  expect: ["Reproduction steps", "Debug console output", "Expected behavior"]

test: "user fills one section"
  oldBody: template with repro steps default, other sections filled
  newBody: template with all sections filled
  expect: ["Reproduction steps"]

test: "user edits but doesn't fill missing sections"
  oldBody: template with repro steps default
  newBody: same template, repro steps still default, only title changed
  expect: []

test: "free-form body to templated body"
  oldBody: "App crashes on login"
  newBody: full template with all sections filled
  expect: ["Reproduction steps", "Debug console output", "Expected behavior", "PWA reproducibility"]

test: "no template in either body"
  oldBody: "Bug report text v1"
  newBody: "Bug report text v2"
  expect: []

test: "user removes content (regression)"
  oldBody: template with repro steps filled
  newBody: template with repro steps now empty
  expect: []  (no items filled, one item unfilled -- not tracked as a signal)
```

### @mention detection (table-driven tests)

Test the mention detection logic (not the full HTTP handler):

```
test: "comment contains bot mention"
  body: "Thanks @ismael-triage-bot, the debug logs helped!"
  expect: true

test: "comment does not contain bot mention"
  body: "I tried the suggested fix but it didn't work"
  expect: false

test: "mention in middle of word"
  body: "not@ismael-triage-bot"
  expect: true (strings.Contains is substring match -- acceptable because GitHub renders this as a mention too)

test: "different case"
  body: "@Ismael-Triage-Bot thanks"
  expect: false (case sensitive)
```

### Store tests

The `RecordFeedbackSignal` and `GetFeedbackStats` methods should have integration tests if the project has a test database setup. If not, they follow simple enough patterns (single INSERT, aggregate SELECT) that manual verification during development is sufficient.

## Estimated Complexity

Migration: trivial (one new table, three indexes). About 10 lines of SQL.

Go code changes across 5 files, roughly:

`internal/github/client.go`: ~15 lines (add `IssueChanges` and `IssueChangeField` structs, add field to `IssueEvent`).

`internal/store/models.go`: ~10 lines (add `FeedbackSignal` struct).

`internal/store/feedback.go`: ~80 lines (new file with `RecordFeedbackSignal`, `GetFeedbackStats`, and the `FeedbackStats`/`RecentFeedback` types).

`internal/store/report.go`: ~10 lines (add `FeedbackStats` field, call `getFeedbackStats` in `GetDashboardStats`).

`internal/webhook/handler.go`: ~60 lines (add `handleEdited` method, add `checkMentionFeedback` method, add `computeFilledSections` helper, wire into `processEvent` and `processCommentEvent`).

`cmd/server/template.html`: ~20 lines (add health card, add feedback table, JS data binding).

Tests: ~80 lines (table-driven tests for `computeFilledSections` and mention detection).

Total: roughly 275 lines of new code across 7 files, plus 1 migration file. One new file (`internal/store/feedback.go`), the rest are edits to existing files. No new dependencies. No LLM calls. No external API calls.

Estimated effort: 1-2 focused sessions. The work is straightforward because it reuses Phase 1 directly and follows established patterns in the codebase.

## File Change Summary

| File | Change |
|---|---|
| `migrations/010_feedback_signals.sql` | New migration: create table + indexes |
| `internal/github/client.go` | Add `IssueChanges`, `IssueChangeField` structs; add `Changes` field to `IssueEvent` |
| `internal/store/models.go` | Add `FeedbackSignal` struct |
| `internal/store/feedback.go` | New file: `RecordFeedbackSignal`, `GetFeedbackStats`, stats types |
| `internal/store/report.go` | Add `FeedbackStats` to `DashboardStats`, wire into `GetDashboardStats` |
| `internal/webhook/handler.go` | Add `handleEdited`, `checkMentionFeedback`, `computeFilledSections`; wire into event routing |
| `internal/webhook/feedback_test.go` | Table-driven tests for `computeFilledSections` and mention detection |
| `cmd/server/template.html` | Add fill rate card and recent feedback panel to triage view |

## Trigger to Start

When we have enough triage data (20+ promoted sessions) to make the metrics meaningful, or when preparing for Stage A to B transition.
