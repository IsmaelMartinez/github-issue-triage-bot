# Phase Q3: Health Monitor

## Summary

A lightweight `/health-check` endpoint that queries operational metrics from the database and returns a structured health report. When any metric crosses a degradation threshold, the endpoint creates a GitHub issue on the bot's own repository to alert the maintainer. This catches silent regressions — such as declining confidence scores or rising agent error rates — that the dashboard only surfaces when someone actively looks at it.

## Data availability

The codebase research reveals the following situation for each proposed metric.

### Confidence score (available now)

The `agent_audit_log` table stores a `confidence_score REAL` column for every agent action. The `getSafetyStats` dashboard query already computes `AVG(confidence_score)`. Comparing a recent-window average against the all-time average is a single SQL query with no schema changes. This is the most ready-to-use metric. Note that the `QualityScore *int` field exists on the `AuditEntry` Go struct but was never backed by a migration — the column does not exist in the database and the judge.go file was never created. The plan should use `confidence_score` as the proxy for output quality rather than the unimplemented quality score.

### Agent session error rate (partially available)

Agent errors are not explicitly recorded in the database. When `HandleComment` or `StartSession` fails, the error is logged via `slog` and an error message is posted as a GitHub comment on the shadow issue, but no failure record is persisted. However, sessions that get stuck in `StageNew` (meaning the context brief failed) are a detectable proxy for errors, since a successful session immediately transitions to `StageContextBrief`. Similarly, sessions stuck in non-terminal stages beyond a reasonable age indicate processing failures. The `agent_sessions.stage` and `agent_sessions.updated_at` columns give us enough to compute a "stuck session" rate without schema changes.

For a more accurate error rate, a new `error_count` column or a dedicated errors table would be needed. The simpler approach — counting stuck sessions — is recommended for the initial implementation.

### Comment posting failure rate (not available)

Comment posting failures are logged but not persisted. When `CreateComment` returns an error in the webhook handler, execution continues and the failure is only visible in Cloud Run logs. There is no database record of attempted-but-failed comment posts.

To track this properly would require either a new `comment_attempts` table or adding an `attempted_at`/`failed` column to `bot_comments`. However, the volume is low enough that a simpler proxy works: compare the count of `triage_sessions` (which represent triage attempts) against the count of `bot_comments` (which represent successful public posts) plus triage sessions that were explicitly rejected or are still pending. A large gap between attempts and outcomes signals posting failures or silent drops.

For the initial implementation, the plan recommends tracking the ratio of triage sessions with no corresponding bot_comment and no explicit closure (`closed_at IS NULL`) that are older than 1 hour. These represent sessions where the promotion flow may have silently failed.

### LLM response time P90 (not available)

The LLM client logs durations to `slog` (`"duration", time.Since(start)`) but does not persist them. There is no table or column for response times. Adding P90 tracking would require either a new `llm_call_log` table or an in-memory ring buffer. Since Cloud Run instances are ephemeral and the bot handles low volume, an in-memory approach would lose data on cold starts.

The recommended approach is to defer this metric to a future iteration and instead track it via Cloud Run's built-in request latency metrics or by adding a simple `llm_calls` table in a later migration. For the initial Q3 implementation, the endpoint should report a placeholder for this metric with a note that it requires instrumentation.

## Implementation steps

### Step 1: Add health check queries to the store layer

File: `internal/store/health.go` (new)

Add a `HealthMetrics` struct and a `GetHealthMetrics(ctx, repo)` method on `*Store`. The method runs four queries in sequence, each independently failable (the endpoint should still return partial results if one query errors).

```go
type HealthMetrics struct {
    ConfidenceRecent7d   *float64 `json:"confidence_recent_7d"`
    ConfidenceAllTime    *float64 `json:"confidence_all_time"`
    StuckSessionCount    int      `json:"stuck_session_count"`
    TotalRecentSessions  int      `json:"total_recent_sessions"`
    OrphanedTriageCount  int      `json:"orphaned_triage_count"`
    TotalTriageSessions  int      `json:"total_triage_sessions"`
    CheckedAt            string   `json:"checked_at"`
}
```

Query 1 — Confidence scores: two subqueries in one round-trip. The recent window uses `WHERE created_at > now() - interval '7 days'`, the all-time average has no time filter. Both scope to `agent_audit_log` joined on `agent_sessions.repo`.

Query 2 — Stuck sessions: count agent sessions not in `('complete')` stage where `updated_at < now() - interval '1 hour'`. Also count total sessions created in the last 30 days for rate calculation.

Query 3 — Orphaned triage sessions: count triage sessions with no matching `bot_comments` row, `closed_at IS NULL`, and `created_at < now() - interval '1 hour'`. Also count total triage sessions in the last 30 days.

### Step 2: Add alert issue creation logic

File: `internal/store/health.go` (same file, or a thin wrapper)

Define a `HealthAlert` struct with `Metric string`, `Current float64`, `Threshold float64`, and `Message string`. The health check logic evaluates each metric against its threshold and collects alerts.

The GitHub issue creation uses the existing `github.Client.CreateIssue` method. To avoid alert spam, the health check should query for an open issue with a specific title prefix (e.g., `[Health Alert]`) before creating a new one. The GitHub client already has `CreateIssue` but does not have a "search issues" method, so a `SearchIssues` method needs to be added.

File: `internal/github/client.go` (modified)

Add `SearchIssues(ctx, installationID, repo, query) ([]IssueSearchResult, error)` that calls `GET /search/issues?q=...`. The query will be scoped to `repo:owner/repo is:open "[Health Alert]" in:title`.

### Step 3: Register the endpoint

File: `cmd/server/main.go` (modified)

Add a new route: `mux.HandleFunc("/health-check", ...)`. The handler needs access to `store`, `ghClient`, and `logger`, following the same pattern as the existing `/cleanup` endpoint. The handler calls `s.GetHealthMetrics(ctx, repo)`, evaluates thresholds, creates GitHub issues for any violations, and returns the full health report as JSON.

The endpoint accepts a `repo` query parameter (defaulting to `IsmaelMartinez/teams-for-linux`) and an `alert_repo` query parameter specifying where to file alert issues (defaulting to the bot's own repo, e.g., `IsmaelMartinez/github-issue-triage-bot`).

The endpoint should require IAP/authentication in the same way as `/cleanup` — it runs on Cloud Run behind identity-aware proxy tokens.

### Step 4: Add a GitHub Actions cron job

File: `.github/workflows/dashboard.yml` (modified)

Add a `health-check` job that runs alongside the existing `cleanup` and `generate` jobs. It authenticates with the same WIF pool and calls `/health-check` via `curl`, matching the pattern of the existing cleanup step. Running once daily (at the same 06:00 UTC cron) is sufficient since the metrics use 7-day windows.

Alternatively, a separate workflow file `.github/workflows/health-check.yml` could be created with a more frequent schedule (e.g., every 6 hours). The daily schedule is recommended initially to keep things simple.

### Step 5: Write tests

File: `internal/store/health_test.go` (new)

Table-driven tests for `GetHealthMetrics` using a test database (matching the pattern of existing store tests if any exist). If the project doesn't have integration test infrastructure for the database, unit tests can verify the alert evaluation logic (threshold comparisons and alert message generation) without a database.

File: `cmd/server/main_test.go` or similar (if endpoint integration tests exist)

Test that the `/health-check` endpoint returns valid JSON and correct HTTP status codes.

## Suggested thresholds

### Confidence score degradation

Threshold: recent 7-day average drops below 80% of the all-time average. For example, if the all-time average confidence is 0.85, an alert fires when the 7-day average falls below 0.68. This relative threshold adapts as the system improves rather than requiring manual adjustment of absolute values.

Rationale: the confidence score is set by the LLM safety validator during research synthesis. A sustained drop indicates either degraded LLM performance, worse input quality, or a regression in the prompt/pipeline. An 80% threshold gives enough headroom for natural variance while catching genuine degradation.

### Stuck session rate

Threshold: more than 2 sessions stuck in non-terminal stages for over 1 hour. At the current volume (a handful of sessions per week), even 1-2 stuck sessions is noteworthy. The absolute count is more useful than a percentage when volumes are this low.

Rationale: a stuck session means either the GitHub API failed during processing, the LLM timed out, or there's a code bug preventing state transitions. At low volumes, any stuck session warrants investigation.

### Orphaned triage rate

Threshold: more than 3 triage sessions older than 1 hour with no corresponding bot_comment and no explicit closure. This is a loose threshold because triage sessions naturally sit in "pending review" state — the alert should only fire for sessions that look like they fell through the cracks, not sessions waiting for a maintainer's `lgtm`.

Rationale: since the shadow repo flow means triage sessions wait for human review before getting a bot_comment, the "1 hour" window filters out sessions in normal review. Sessions older than 1 hour without any resolution at all (neither promoted, rejected, nor marked closed) suggest a posting failure or a process gap.

### LLM response time P90

Deferred to a future iteration. If instrumentation is added later, a reasonable starting threshold would be P90 above 30 seconds (the Gemini API timeout is 60 seconds, so a P90 at 30s suggests the system is approaching timeout territory regularly).

## Testing approach

The health check queries touch existing tables with no schema changes, so they can be tested against the production database in a read-only manner (the endpoint itself only writes when creating a GitHub alert issue).

For the alert creation path, the test should verify that the `SearchIssues` call prevents duplicate alert issues. This can be tested by mocking the GitHub client interface or by using a test repository.

The threshold evaluation logic should be extracted into a pure function `evaluateThresholds(metrics HealthMetrics) []HealthAlert` that is trivially unit-testable without any database or API dependencies.

## Estimated complexity

The implementation touches 4-5 files:

`internal/store/health.go` (new, ~120 lines) contains the health metrics queries and the HealthMetrics struct. `internal/github/client.go` (modified, ~30 lines added) gains a SearchIssues method for deduplicating alert issues. `cmd/server/main.go` (modified, ~50 lines added) registers the /health-check endpoint handler with threshold evaluation and alert creation. `.github/workflows/dashboard.yml` (modified, ~10 lines added) adds a health-check step to the daily cron. `internal/store/health_test.go` (new, ~80 lines) covers threshold evaluation logic.

No new database migrations are required. The entire feature reads from existing tables. Total estimated effort is roughly half a day of implementation and testing.

## Future enhancements

Once the basic health monitor is running, two extensions become natural next steps. First, adding an `llm_calls` table (migration 010) to track LLM request duration, model name, and success/failure would enable the P90 latency metric and also give visibility into Gemini API reliability over time. Second, the quality score infrastructure (judge.go and migration 005) that was planned but never implemented could be revived, giving the health monitor a true quality signal rather than the confidence score proxy.
