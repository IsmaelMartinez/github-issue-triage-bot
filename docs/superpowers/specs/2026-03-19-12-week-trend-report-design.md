# 12-Week Trend Report Design

Date: 2026-03-19

## Problem

The bot has been running in shadow mode for about a week with 11 triage sessions and zero feedback signals. Making decisions about public posting, threshold tuning, and Month 3 prioritisation requires trend data over time, not just point-in-time snapshots. The current `/report` endpoint and dashboard show totals and recent activity but no weekly trends. A 12-week trend view would answer: is triage quality improving? Are phase hit rates stable? Is the synthesis engine producing signal? When is the data sufficient to justify the Stage A to B transition?

## Approach

Extend the existing `/report` JSON API and live dashboard with weekly-bucketed trend data. No new tables or migrations — all metrics come from `date_trunc('week', created_at)` aggregations against existing tables. The dashboard adds a "Trends" tab with Chart.js line charts (Chart.js is already loaded). One small addition to the synthesis runner records a journal event when a briefing is posted, so briefing counts appear in the trends without a dedicated table.

## Data Layer

A single new store method in `internal/store/report.go`:

```go
func (s *Store) GetWeeklyTrends(ctx context.Context, repo string, weeks int) (*WeeklyTrends, error)
```

Returns a `WeeklyTrends` struct containing six metric slices, each bucketed by ISO week start date. Weeks with no data are zero-filled using `generate_series` (same pattern as existing `daily_triage_counts` in `GetDashboardStats`).

### Metric Groups (priority order)

1. Triage volume and promotion rate (`triage_sessions` table): total sessions per week, promoted count, and rate as a float. This is the primary "readiness for public" signal.

2. Phase hit rates (`triage_sessions.phases_run` array): each phase's hit count divided by total sessions per week. Unnest the `phases_run` text array and count per phase. Useful for spotting whether thresholds need tuning.

3. Average response time (`issues.created_at` vs `triage_sessions.created_at`): the time delta between issue creation on GitHub and triage session creation, averaged per week. Should stay flat or improve.

4. Agent session activity (`agent_sessions` table): total sessions per week with a stage breakdown (approved, rejected, pending, complete). Shows whether the enhancement research track is producing useful output.

5. Synthesis briefings (`repo_events` where `event_type = 'briefing_posted'`): count of briefings posted per week, plus total findings from the event metadata. Will be zero initially and populate as the Monday cron runs.

6. Feedback signals (`feedback_signals` table): count per week by signal type (edit_fill, mention). Zero while in shadow mode; the trend line is ready for when data flows in.

### Types

```go
type WeeklyTrends struct {
    Repo  string       `json:"repo"`
    Weeks int          `json:"weeks"`
    Triage     []WeeklyTriage     `json:"triage"`
    Phases     []WeeklyPhases     `json:"phases"`
    Response   []WeeklyResponse   `json:"response_time"`
    Agents     []WeeklyAgents     `json:"agents"`
    Synthesis  []WeeklySynthesis  `json:"synthesis"`
    Feedback   []WeeklyFeedback   `json:"feedback"`
}

type WeeklyTriage struct {
    Week     string  `json:"week"`
    Total    int     `json:"total"`
    Promoted int     `json:"promoted"`
    Rate     float64 `json:"rate"`
}

type WeeklyPhases struct {
    Week    string  `json:"week"`
    Phase1  float64 `json:"phase1"`
    Phase2  float64 `json:"phase2"`
    Phase4a float64 `json:"phase4a"`
}

type WeeklyResponse struct {
    Week       string  `json:"week"`
    AvgSeconds float64 `json:"avg_seconds"`
}

type WeeklyAgents struct {
    Week     string `json:"week"`
    Total    int    `json:"total"`
    Approved int    `json:"approved"`
    Rejected int    `json:"rejected"`
    Pending  int    `json:"pending"`
    Complete int    `json:"complete"`
}

type WeeklySynthesis struct {
    Week      string `json:"week"`
    Briefings int    `json:"briefings"`
    Findings  int    `json:"findings"`
}

type WeeklyFeedback struct {
    Week      string `json:"week"`
    EditFills int    `json:"edit_fills"`
    Mentions  int    `json:"mentions"`
}
```

### Query Pattern

Each metric group is a separate SQL query within `GetWeeklyTrends`. If an individual query fails, return partial results with the error joined (same pattern as `GetHealthMetrics` using `errors.Join`). The week series is generated with:

The week cutoff is computed in Go (`time.Now().Add(-time.Duration(weeks) * 7 * 24 * time.Hour)`) and passed as a `$1` timestamp parameter, avoiding SQL string interpolation:

```sql
SELECT date_trunc('week', d)::date AS week
FROM generate_series(
    date_trunc('week', $1::timestamptz),
    date_trunc('week', NOW()),
    '1 week'::interval
) d
```

All timestamps use UTC. The Neon database runs in UTC and `date_trunc('week', ...)` returns Monday-based ISO weeks. Week labels are formatted as `"2006-01-02"` (ISO date string, same as existing `DailyBucket.Date` format). Client-side Chart.js formatting converts to human-readable labels like "Mar 3".

Each metric query LEFT JOINs against this series to ensure zero-filled rows for weeks without activity. The response time query includes a `t.created_at > i.created_at` guard to prevent negative deltas (same guard as the existing scalar response time query in `GetDashboardStats`).

## API Layer

### Endpoint

`GET /report/trends?repo=X&weeks=12`

Registered in `cmd/server/main.go` alongside the existing `/report` handler. Same auth pattern: `allowedRepos` check, defaults to `IsmaelMartinez/teams-for-linux`.

The `weeks` parameter is optional, defaults to 12, capped at 52. Values below 1 are clamped to 1.

Response is the `WeeklyTrends` struct serialised as JSON with `Content-Type: application/json`.

### Error Handling

If `GetWeeklyTrends` returns partial data with errors, the endpoint returns HTTP 200 with a `"partial": true` field (same pattern as `/health-check`). If the store returns nil data, return HTTP 500.

## Dashboard UI

### Navigation

Add a "Trends" item to the sidebar navigation in `cmd/server/template.html`, after the existing "Overview" tab. Clicking it shows the trends panel and hides the overview panel. Same toggle pattern as the existing sidebar navigation.

### Layout

Three chart rows, priority-ordered top to bottom.

Row 1 — Triage Readiness (full width): A dual-axis Chart.js chart. Left axis (bar) shows triage session count per week. Right axis (line) shows promotion rate as a percentage. Labels are week start dates formatted as "Mar 3", "Mar 10", etc. This is the primary decision-making chart.

Row 2 — Operational Health (two charts, side by side): Left chart shows phase hit rates as lines (phase1, phase2, phase4a) over time, each a different colour. Right chart shows average response time per week as a single line.

Row 3 — Emerging Metrics (two charts, side by side): Left chart shows agent sessions per week with approved/rejected as stacked bars. Right chart shows synthesis briefings and feedback signals as lines — separate series for briefings, edit_fills, and mentions.

### Empty State

For metric groups with all-zero data across the entire window, the chart area displays a muted grey message: "No data yet" instead of rendering a flat zero line. This makes it clear the absence is expected rather than a bug.

### Data Fetching

When the Trends tab is selected, a single `fetch('/report/trends?repo=' + currentRepo + '&weeks=12')` call retrieves all data. Charts are rendered on completion. Same async pattern as the existing dashboard data loading.

## Synthesis Event Recording

In `internal/synthesis/runner.go`, after a successful `CreateIssue` call for the briefing, add a journal event:

```go
if h.store != nil {
    _ = h.store.RecordEvent(ctx, store.RepoEvent{
        Repo:      repo,
        EventType: "briefing_posted",
        SourceRef: fmt.Sprintf("#%d", issueNumber),
        Summary:   title,
        Metadata:  map[string]any{"findings": len(allFindings)},
    })
}
```

Best-effort: log errors but don't fail the briefing. This requires adding a `store *store.Store` field to the `Runner` struct and updating the constructor signature:

```go
func NewRunner(github *gh.Client, store *store.Store, logger *slog.Logger, synthesizers ...Synthesizer) *Runner
```

The store is passed as `*store.Store` directly (not a narrower interface) since `RecordEvent` is the only method needed and introducing an interface for one call would be over-engineering. The `/synthesize` handler in `cmd/server/main.go` already has access to `s` (the store) and passes it to the updated constructor.

## Testing

Unit tests for the trend types in `internal/store/report_test.go`: table-driven tests verifying struct shape, weeks parameter clamping logic, and type construction. These are pure Go tests with no database dependency (same pattern as `TestRepoEventModel`).

Zero-fill behaviour (generate_series + LEFT JOIN) can only be verified with a live database. If integration testing is needed, add tests to `internal/store/integration_test.go` following the existing pattern (requires `DATABASE_URL`). This is optional for the initial implementation — the generate_series pattern is well-established in the codebase via `GetDashboardStats`.

Endpoint test in `cmd/server/trends_test.go`: verify auth (missing/invalid repo returns 403), parameter validation (weeks defaults to 12, capped at 52), and JSON response shape.

Dashboard template changes verified visually after deploy — no automated UI tests (consistent with existing dashboard approach).

## Files Changed

- `internal/store/report.go` — add `GetWeeklyTrends` method and trend types
- `internal/store/report_test.go` — add trend unit tests
- `internal/synthesis/runner.go` — add store field to Runner, record briefing event
- `cmd/server/main.go` — add `/report/trends` endpoint, pass store to synthesis Runner
- `cmd/server/template.html` — add Trends tab with Chart.js charts
- `cmd/server/trends_test.go` — endpoint auth and parameter tests

## What This Does Not Include

- New database migrations (all queries use existing tables)
- New external dependencies (Chart.js already loaded)
- Public posting or Stage A to B transition (separate design)
- Kill switch or LLM rate enforcement (deferred, not needed in shadow mode)
- Synthesis quality tuning (needs real data from weekly runs first)
