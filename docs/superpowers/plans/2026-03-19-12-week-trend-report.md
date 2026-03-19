# 12-Week Trend Report Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 12-week trend reporting to the dashboard and API, showing weekly-bucketed triage, phase, response time, agent, synthesis, and feedback metrics.

**Architecture:** One new store method (`GetWeeklyTrends`) with six SQL queries using `generate_series` + LEFT JOIN for zero-fill. One new `/report/trends` GET endpoint. One new "Trends" sidebar tab in the dashboard with Chart.js charts. One small change to the synthesis runner to record briefing events.

**Tech Stack:** Go 1.26, PostgreSQL (date_trunc, generate_series), Chart.js 4 (already loaded), HTML template (go:embed)

**Spec:** `docs/superpowers/specs/2026-03-19-12-week-trend-report-design.md`

---

## Task 1: Trend Types and Weeks Clamping Helper

**Files:**
- Modify: `internal/store/report.go` — add trend type definitions
- Modify: `internal/store/report_test.go` — add type and clamping tests

- [ ] **Step 1: Write failing tests for trend types and clamping**

Append only the test functions below to the existing `internal/store/report_test.go` file (do NOT duplicate the `package store` or `import` lines — the file already has them):

```go
func TestClampWeeks(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"default zero", 0, 12},
		{"negative", -5, 12},
		{"normal", 8, 8},
		{"max", 52, 52},
		{"over max", 100, 52},
		{"one", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampWeeks(tt.input); got != tt.want {
				t.Errorf("ClampWeeks(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestWeeklyTriageType(t *testing.T) {
	wt := WeeklyTriage{Week: "2026-03-10", Total: 5, Promoted: 2, Rate: 0.4}
	if wt.Week != "2026-03-10" {
		t.Errorf("Week = %q", wt.Week)
	}
	if wt.Rate != 0.4 {
		t.Errorf("Rate = %f", wt.Rate)
	}
}

func TestWeeklyAgentsType(t *testing.T) {
	wa := WeeklyAgents{Week: "2026-03-10", Total: 3, Approved: 1, Rejected: 1, Pending: 0, Complete: 1}
	if wa.Total != 3 {
		t.Errorf("Total = %d", wa.Total)
	}
	if wa.Approved+wa.Rejected+wa.Complete != 3 {
		t.Errorf("stage sum mismatch")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run "TestClampWeeks|TestWeeklyTriageType|TestWeeklyAgentsType" -v`
Expected: FAIL — `ClampWeeks`, `WeeklyTriage`, `WeeklyAgents` not defined

- [ ] **Step 3: Implement types and ClampWeeks**

Add to `internal/store/report.go` before the `GetDashboardStats` function (around line 108). Note: `FeedbackStats` is defined in `feedback.go`, not in this file.

```go
// ClampWeeks normalises a user-provided weeks parameter to [1, 52], defaulting 0 or negative to 12.
func ClampWeeks(weeks int) int {
	if weeks <= 0 {
		return 12
	}
	if weeks > 52 {
		return 52
	}
	return weeks
}

// WeeklyTrends holds all weekly-bucketed trend data for the /report/trends endpoint.
type WeeklyTrends struct {
	Repo      string            `json:"repo"`
	Weeks     int               `json:"weeks"`
	Triage    []WeeklyTriage    `json:"triage"`
	Phases    []WeeklyPhases    `json:"phases"`
	Response  []WeeklyResponse  `json:"response_time"`
	Agents    []WeeklyAgents    `json:"agents"`
	Synthesis []WeeklySynthesis `json:"synthesis"`
	Feedback  []WeeklyFeedback  `json:"feedback"`
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run "TestClampWeeks|TestWeeklyTriageType|TestWeeklyAgentsType" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/report.go internal/store/report_test.go
git commit -m "feat: add weekly trend types and ClampWeeks helper"
```

---

## Task 2: GetWeeklyTrends Store Method — Triage and Phase Queries

**Files:**
- Modify: `internal/store/report.go` — add GetWeeklyTrends with triage and phase queries

The cutoff timestamp is computed in Go and passed as a parameter. Each sub-query is separate — if one fails, the method logs and continues (partial results with errors.Join, same pattern as GetHealthMetrics in `internal/store/health.go`).

- [ ] **Step 1: Implement GetWeeklyTrends with triage volume + promotion rate query**

Add to `internal/store/report.go` after the type definitions:

```go
// GetWeeklyTrends returns weekly-bucketed trend data for a repo over the given number of weeks.
// Individual metric queries that fail are skipped; partial results are returned with joined errors.
func (s *Store) GetWeeklyTrends(ctx context.Context, repo string, weeks int) (*WeeklyTrends, error) {
	weeks = ClampWeeks(weeks)
	cutoff := time.Now().Add(-time.Duration(weeks) * 7 * 24 * time.Hour)

	trends := &WeeklyTrends{
		Repo:  repo,
		Weeks: weeks,
	}
	var errs []error

	// 1. Triage volume + promotion rate
	triageRows, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(t.total, 0),
			COALESCE(t.promoted, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', ts.created_at) AS week,
				COUNT(*) AS total,
				COUNT(bc.issue_number) AS promoted
			FROM triage_sessions ts
			LEFT JOIN bot_comments bc ON ts.repo = bc.repo AND ts.issue_number = bc.issue_number
			WHERE ts.repo = $2 AND ts.created_at >= $1::timestamptz
			GROUP BY date_trunc('week', ts.created_at)
		) t ON t.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("triage trends: %w", err))
	} else {
		defer triageRows.Close()
		for triageRows.Next() {
			var wt WeeklyTriage
			var d time.Time
			if err := triageRows.Scan(&d, &wt.Total, &wt.Promoted); err != nil {
				errs = append(errs, fmt.Errorf("scan triage trend: %w", err))
				break
			}
			wt.Week = d.Format("2006-01-02")
			if wt.Total > 0 {
				wt.Rate = float64(wt.Promoted) / float64(wt.Total)
			}
			trends.Triage = append(trends.Triage, wt)
		}
		if triageRows.Err() != nil {
			errs = append(errs, triageRows.Err())
		}
	}
	if trends.Triage == nil {
		trends.Triage = []WeeklyTriage{}
	}

	// 2. Phase hit rates
	phaseRows, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(p.phase1, 0),
			COALESCE(p.phase2, 0),
			COALESCE(p.phase4a, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', ts.created_at) AS week,
				COUNT(CASE WHEN 'phase1' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase1,
				COUNT(CASE WHEN 'phase2' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase2,
				COUNT(CASE WHEN 'phase4a' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase4a
			FROM triage_sessions ts
			WHERE ts.repo = $2 AND ts.created_at >= $1::timestamptz
			GROUP BY date_trunc('week', ts.created_at)
		) p ON p.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("phase trends: %w", err))
	} else {
		defer phaseRows.Close()
		for phaseRows.Next() {
			var wp WeeklyPhases
			var d time.Time
			if err := phaseRows.Scan(&d, &wp.Phase1, &wp.Phase2, &wp.Phase4a); err != nil {
				errs = append(errs, fmt.Errorf("scan phase trend: %w", err))
				break
			}
			wp.Week = d.Format("2006-01-02")
			trends.Phases = append(trends.Phases, wp)
		}
		if phaseRows.Err() != nil {
			errs = append(errs, phaseRows.Err())
		}
	}
	if trends.Phases == nil {
		trends.Phases = []WeeklyPhases{}
	}

	// Remaining queries (3-6) added in Task 3
	trends.Response = []WeeklyResponse{}
	trends.Agents = []WeeklyAgents{}
	trends.Synthesis = []WeeklySynthesis{}
	trends.Feedback = []WeeklyFeedback{}

	return trends, errors.Join(errs...)
}
```

Add `"fmt"` to the imports if not already present (it's used for error wrapping).

- [ ] **Step 2: Run go vet**

Run: `go vet ./internal/store/`
Expected: clean

- [ ] **Step 3: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: add GetWeeklyTrends with triage volume and phase hit rate queries"
```

---

## Task 3: GetWeeklyTrends — Response Time, Agent, Synthesis, Feedback Queries

**Files:**
- Modify: `internal/store/report.go` — complete the remaining four queries in GetWeeklyTrends

Replace the placeholder slices (Task 2's `trends.Response = []WeeklyResponse{}` etc.) with the actual queries.

- [ ] **Step 1: Add response time, agent, synthesis, and feedback queries**

Replace the `// Remaining queries (3-6) added in Task 3` block and the four placeholder assignments with:

```go
	// 3. Average response time per week
	respRows, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(r.avg_secs, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', t.created_at) AS week,
				AVG(EXTRACT(EPOCH FROM (t.created_at - i.created_at))) AS avg_secs
			FROM triage_sessions t
			INNER JOIN issues i ON t.repo = i.repo AND t.issue_number = i.number
			WHERE t.repo = $2 AND t.created_at >= $1::timestamptz AND t.created_at > i.created_at
			GROUP BY date_trunc('week', t.created_at)
		) r ON r.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("response time trends: %w", err))
	} else {
		defer respRows.Close()
		for respRows.Next() {
			var wr WeeklyResponse
			var d time.Time
			if err := respRows.Scan(&d, &wr.AvgSeconds); err != nil {
				errs = append(errs, fmt.Errorf("scan response trend: %w", err))
				break
			}
			wr.Week = d.Format("2006-01-02")
			trends.Response = append(trends.Response, wr)
		}
		if respRows.Err() != nil {
			errs = append(errs, respRows.Err())
		}
	}
	if trends.Response == nil {
		trends.Response = []WeeklyResponse{}
	}

	// 4. Agent session activity
	agentRows, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(a.total, 0),
			COALESCE(a.approved, 0),
			COALESCE(a.rejected, 0),
			COALESCE(a.pending, 0),
			COALESCE(a.complete, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(*) AS total,
				COUNT(CASE WHEN stage = 'approved' THEN 1 END) AS approved,
				COUNT(CASE WHEN stage = 'revision' THEN 1 END) AS rejected,
				COUNT(CASE WHEN stage IN ('new', 'clarifying', 'researching', 'review_pending', 'context_brief') THEN 1 END) AS pending,
				COUNT(CASE WHEN stage = 'complete' THEN 1 END) AS complete
			FROM agent_sessions
			WHERE repo = $2 AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) a ON a.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("agent trends: %w", err))
	} else {
		defer agentRows.Close()
		for agentRows.Next() {
			var wa WeeklyAgents
			var d time.Time
			if err := agentRows.Scan(&d, &wa.Total, &wa.Approved, &wa.Rejected, &wa.Pending, &wa.Complete); err != nil {
				errs = append(errs, fmt.Errorf("scan agent trend: %w", err))
				break
			}
			wa.Week = d.Format("2006-01-02")
			trends.Agents = append(trends.Agents, wa)
		}
		if agentRows.Err() != nil {
			errs = append(errs, agentRows.Err())
		}
	}
	if trends.Agents == nil {
		trends.Agents = []WeeklyAgents{}
	}

	// 5. Synthesis briefings (from event journal)
	synthRows, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(b.briefings, 0),
			COALESCE(b.findings, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(*) AS briefings,
				COALESCE(SUM((metadata->>'findings')::int), 0) AS findings
			FROM repo_events
			WHERE repo = $2 AND event_type = 'briefing_posted' AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) b ON b.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("synthesis trends: %w", err))
	} else {
		defer synthRows.Close()
		for synthRows.Next() {
			var ws WeeklySynthesis
			var d time.Time
			if err := synthRows.Scan(&d, &ws.Briefings, &ws.Findings); err != nil {
				errs = append(errs, fmt.Errorf("scan synthesis trend: %w", err))
				break
			}
			ws.Week = d.Format("2006-01-02")
			trends.Synthesis = append(trends.Synthesis, ws)
		}
		if synthRows.Err() != nil {
			errs = append(errs, synthRows.Err())
		}
	}
	if trends.Synthesis == nil {
		trends.Synthesis = []WeeklySynthesis{}
	}

	// 6. Feedback signals
	fbRows, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(f.edit_fills, 0),
			COALESCE(f.mentions, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(CASE WHEN signal_type = 'issue_edit_filled' THEN 1 END) AS edit_fills,
				COUNT(CASE WHEN signal_type = 'user_mention' THEN 1 END) AS mentions
			FROM feedback_signals
			WHERE repo = $2 AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) f ON f.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("feedback trends: %w", err))
	} else {
		defer fbRows.Close()
		for fbRows.Next() {
			var wf WeeklyFeedback
			var d time.Time
			if err := fbRows.Scan(&d, &wf.EditFills, &wf.Mentions); err != nil {
				errs = append(errs, fmt.Errorf("scan feedback trend: %w", err))
				break
			}
			wf.Week = d.Format("2006-01-02")
			trends.Feedback = append(trends.Feedback, wf)
		}
		if fbRows.Err() != nil {
			errs = append(errs, fbRows.Err())
		}
	}
	if trends.Feedback == nil {
		trends.Feedback = []WeeklyFeedback{}
	}
```

- [ ] **Step 2: Run go vet**

Run: `go vet ./internal/store/`
Expected: clean

- [ ] **Step 3: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: complete GetWeeklyTrends with response time, agent, synthesis, feedback queries"
```

---

## Task 4: /report/trends Endpoint

**Files:**
- Modify: `cmd/server/main.go` — add `/report/trends` handler
- Create: `cmd/server/trends_test.go` — endpoint tests

- [ ] **Step 1: Write failing tests for the endpoint**

```go
// cmd/server/trends_test.go
package main

import "testing"

func TestParseWeeksParam(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty defaults to 12", "", 12},
		{"valid", "8", 8},
		{"too high clamped", "100", 52},
		{"non-numeric defaults", "abc", 12},
		{"zero defaults", "0", 12},
		{"negative defaults", "-5", 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseWeeksParam(tt.input); got != tt.want {
				t.Errorf("parseWeeksParam(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/server/ -run TestParseWeeksParam -v`
Expected: FAIL — `parseWeeksParam` not defined

- [ ] **Step 3: Implement endpoint and helper**

Add `parseWeeksParam` function to `cmd/server/main.go` (after `validateIngestAuth`):

```go
func parseWeeksParam(s string) int {
	if s == "" {
		return store.ClampWeeks(0)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return store.ClampWeeks(0)
	}
	return store.ClampWeeks(n)
}
```

Add the endpoint in `cmd/server/main.go` after the `/report` handler (around line 320, before the `/api/triage/` handler):

```go
mux.HandleFunc("/report/trends", func(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}
	if !allowedRepos[repo] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	weeks := parseWeeksParam(r.URL.Query().Get("weeks"))
	trends, trendsErr := s.GetWeeklyTrends(r.Context(), repo, weeks)
	if trends == nil {
		http.Error(w, "failed to get trends", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		*store.WeeklyTrends
		Partial bool `json:"partial"`
	}{
		WeeklyTrends: trends,
		Partial:      trendsErr != nil,
	}
	if trendsErr != nil {
		logger.Warn("partial weekly trends", "error", trendsErr, "repo", repo)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error("encoding trends response", "error", err)
	}
})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/server/ -run TestParseWeeksParam -v`
Expected: PASS

- [ ] **Step 5: Run go vet on full project**

Run: `go vet ./...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go cmd/server/trends_test.go
git commit -m "feat: add /report/trends endpoint with weeks parameter validation"
```

---

## Task 5: Synthesis Runner — Record Briefing Events

**Files:**
- Modify: `internal/synthesis/runner.go` — add store field, record event on briefing post
- Modify: `cmd/server/main.go` — pass store to NewRunner

- [ ] **Step 1: Update Runner struct and constructor**

In `internal/synthesis/runner.go`, add store import and field:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

type Runner struct {
	synthesizers []Synthesizer
	github       *gh.Client
	store        *store.Store
	logger       *slog.Logger
}

func NewRunner(github *gh.Client, s *store.Store, logger *slog.Logger, synthesizers ...Synthesizer) *Runner {
	return &Runner{synthesizers: synthesizers, github: github, store: s, logger: logger}
}
```

- [ ] **Step 2: Record briefing event after posting**

In the `Run` method, after the successful `CreateIssue` call, replace the existing log line with:

```go
	issueNumber, err := r.github.CreateIssue(ctx, installationID, shadowRepo, title, briefing)
	if err != nil {
		return len(allFindings), fmt.Errorf("posting briefing: %w", err)
	}

	// Record briefing event in journal (best-effort)
	if r.store != nil {
		if evErr := r.store.RecordEvent(ctx, store.RepoEvent{
			Repo:      repo,
			EventType: "briefing_posted",
			SourceRef: fmt.Sprintf("#%d", issueNumber),
			Summary:   title,
			Metadata:  map[string]any{"findings": len(allFindings)},
		}); evErr != nil {
			r.logger.Error("recording briefing event", "error", evErr)
		}
	}

	r.logger.Info("briefing posted", "repo", repo, "findings", len(allFindings))
	return len(allFindings), nil
```

- [ ] **Step 3: Update NewRunner call in cmd/server/main.go**

In the `/synthesize` handler, change:

```go
runner := synthesis.NewRunner(ghClient, logger, clusterSynth, driftSynth, upstreamSynth)
```

to:

```go
runner := synthesis.NewRunner(ghClient, s, logger, clusterSynth, driftSynth, upstreamSynth)
```

- [ ] **Step 4: Run full test suite**

Run: `go vet ./... && go test ./... -count=1 -short`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/synthesis/runner.go cmd/server/main.go
git commit -m "feat: record briefing_posted event in journal for synthesis trend tracking"
```

---

## Task 6: Dashboard — Trends Tab with Chart.js Charts

**Files:**
- Modify: `cmd/server/template.html` — add Trends sidebar nav item and chart panel

This is the largest single task. The dashboard template uses go:embed and Chart.js 4 (already loaded via CDN at line 75). The sidebar nav pattern is at line 86-90 using `showView('name', this)`. Views use `<div id="view-name" class="view">`.

- [ ] **Step 1: Add Trends nav item to sidebar**

In `cmd/server/template.html`, after the "System" nav item (line 90), add:

```html
<a href="#trends" onclick="showView('trends',this);return false"><span class="dot" id="dot-trends"></span> Trends</a>
```

- [ ] **Step 2: Add Trends view panel**

After the last `</div><!-- view-system -->` closing tag, add the trends view with all chart containers:

```html
<div id="view-trends" class="view">
    <h2>12-Week Trends</h2>

    <div class="panel">
        <h3>Triage Readiness</h3>
        <div class="chart-container" style="height:200px"><canvas id="chart-triage-trend"></canvas></div>
        <div id="triage-trend-empty" style="display:none;color:#888;text-align:center;padding:40px 0;font-size:0.85rem">No triage data yet</div>
    </div>

    <div class="two-col">
        <div class="panel">
            <h3>Phase Hit Rates</h3>
            <div class="chart-container"><canvas id="chart-phase-trend"></canvas></div>
            <div id="phase-trend-empty" style="display:none;color:#888;text-align:center;padding:40px 0;font-size:0.85rem">No phase data yet</div>
        </div>
        <div class="panel">
            <h3>Avg Response Time</h3>
            <div class="chart-container"><canvas id="chart-response-trend"></canvas></div>
            <div id="response-trend-empty" style="display:none;color:#888;text-align:center;padding:40px 0;font-size:0.85rem">No response data yet</div>
        </div>
    </div>

    <div class="two-col">
        <div class="panel">
            <h3>Agent Sessions</h3>
            <div class="chart-container"><canvas id="chart-agent-trend"></canvas></div>
            <div id="agent-trend-empty" style="display:none;color:#888;text-align:center;padding:40px 0;font-size:0.85rem">No agent data yet</div>
        </div>
        <div class="panel">
            <h3>Synthesis &amp; Feedback</h3>
            <div class="chart-container"><canvas id="chart-synth-trend"></canvas></div>
            <div id="synth-trend-empty" style="display:none;color:#888;text-align:center;padding:40px 0;font-size:0.85rem">No synthesis or feedback data yet</div>
        </div>
    </div>
</div>
```

- [ ] **Step 3: Add trends data fetching and chart rendering JavaScript**

In the `<script>` section, find the existing `showView` function (line 159). Add the following line at the end of the function body, after the existing `initFeedbackChart` conditional:

```javascript
if (n === 'trends' && typeof loadTrends === 'function') loadTrends();
```

Do NOT replace the entire `showView` function — only append this one branch.

Add the `loadTrends` function and chart builders in the script section:

```javascript
var trendsLoaded = false;
function loadTrends() {
    if (trendsLoaded) return;
    var repo = document.getElementById('repo-select') ? document.getElementById('repo-select').value : '{{.Repo}}';
    fetch('/report/trends?repo=' + encodeURIComponent(repo) + '&weeks=12')
        .then(function(r) { return r.json(); })
        .then(function(data) {
            trendsLoaded = true;
            renderTriageTrend(data.triage);
            renderPhaseTrend(data.phases);
            renderResponseTrend(data.response_time);
            renderAgentTrend(data.agents);
            renderSynthTrend(data.synthesis, data.feedback);
        })
        .catch(function(e) { console.error('Failed to load trends:', e); });
}

function fmtWeek(iso) {
    var d = new Date(iso + 'T00:00:00');
    return d.toLocaleDateString('en-GB', { month: 'short', day: 'numeric' });
}

function isAllZero(arr, keys) {
    return arr.every(function(row) { return keys.every(function(k) { return (row[k] || 0) === 0; }); });
}

function showEmpty(chartId, emptyId, isEmpty) {
    document.getElementById(chartId).parentElement.style.display = isEmpty ? 'none' : 'block';
    document.getElementById(emptyId).style.display = isEmpty ? 'block' : 'none';
}

function renderTriageTrend(data) {
    if (isAllZero(data, ['total'])) { showEmpty('chart-triage-trend', 'triage-trend-empty', true); return; }
    showEmpty('chart-triage-trend', 'triage-trend-empty', false);
    new Chart(document.getElementById('chart-triage-trend'), {
        data: {
            labels: data.map(function(d) { return fmtWeek(d.week); }),
            datasets: [
                { type: 'bar', label: 'Sessions', data: data.map(function(d) { return d.total; }), backgroundColor: 'rgba(46,164,79,0.5)', yAxisID: 'y' },
                { type: 'line', label: 'Promotion %', data: data.map(function(d) { return Math.round(d.rate * 100); }), borderColor: '#0366d6', yAxisID: 'y1', tension: 0.3 }
            ]
        },
        options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true, title: { display: true, text: 'Sessions' } }, y1: { position: 'right', beginAtZero: true, max: 100, title: { display: true, text: '%' }, grid: { drawOnChartArea: false } } } }
    });
}

function renderPhaseTrend(data) {
    if (isAllZero(data, ['phase1', 'phase2', 'phase4a'])) { showEmpty('chart-phase-trend', 'phase-trend-empty', true); return; }
    showEmpty('chart-phase-trend', 'phase-trend-empty', false);
    new Chart(document.getElementById('chart-phase-trend'), {
        type: 'line',
        data: {
            labels: data.map(function(d) { return fmtWeek(d.week); }),
            datasets: [
                { label: 'Phase 1', data: data.map(function(d) { return Math.round(d.phase1 * 100); }), borderColor: '#2ea44f', tension: 0.3 },
                { label: 'Phase 2', data: data.map(function(d) { return Math.round(d.phase2 * 100); }), borderColor: '#0366d6', tension: 0.3 },
                { label: 'Phase 4a', data: data.map(function(d) { return Math.round(d.phase4a * 100); }), borderColor: '#6f42c1', tension: 0.3 }
            ]
        },
        options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true, max: 100, title: { display: true, text: 'Hit Rate %' } } } }
    });
}

function renderResponseTrend(data) {
    if (isAllZero(data, ['avg_seconds'])) { showEmpty('chart-response-trend', 'response-trend-empty', true); return; }
    showEmpty('chart-response-trend', 'response-trend-empty', false);
    new Chart(document.getElementById('chart-response-trend'), {
        type: 'line',
        data: {
            labels: data.map(function(d) { return fmtWeek(d.week); }),
            datasets: [{ label: 'Avg (seconds)', data: data.map(function(d) { return Math.round(d.avg_seconds * 10) / 10; }), borderColor: '#e36209', fill: true, backgroundColor: 'rgba(227,98,9,0.1)', tension: 0.3 }]
        },
        options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true, title: { display: true, text: 'Seconds' } } } }
    });
}

function renderAgentTrend(data) {
    if (isAllZero(data, ['total'])) { showEmpty('chart-agent-trend', 'agent-trend-empty', true); return; }
    showEmpty('chart-agent-trend', 'agent-trend-empty', false);
    new Chart(document.getElementById('chart-agent-trend'), {
        type: 'bar',
        data: {
            labels: data.map(function(d) { return fmtWeek(d.week); }),
            datasets: [
                { label: 'Approved', data: data.map(function(d) { return d.approved; }), backgroundColor: '#2ea44f' },
                { label: 'Complete', data: data.map(function(d) { return d.complete; }), backgroundColor: '#0366d6' },
                { label: 'Pending', data: data.map(function(d) { return d.pending; }), backgroundColor: '#dbab09' },
                { label: 'Rejected', data: data.map(function(d) { return d.rejected; }), backgroundColor: '#da3633' }
            ]
        },
        options: { responsive: true, maintainAspectRatio: false, scales: { x: { stacked: true }, y: { stacked: true, beginAtZero: true } } }
    });
}

function renderSynthTrend(synthData, fbData) {
    var allEmpty = isAllZero(synthData, ['briefings', 'findings']) && isAllZero(fbData, ['edit_fills', 'mentions']);
    if (allEmpty) { showEmpty('chart-synth-trend', 'synth-trend-empty', true); return; }
    showEmpty('chart-synth-trend', 'synth-trend-empty', false);
    new Chart(document.getElementById('chart-synth-trend'), {
        type: 'line',
        data: {
            labels: synthData.map(function(d) { return fmtWeek(d.week); }),
            datasets: [
                { label: 'Briefings', data: synthData.map(function(d) { return d.briefings; }), borderColor: '#6f42c1', tension: 0.3 },
                { label: 'Findings', data: synthData.map(function(d) { return d.findings; }), borderColor: '#0366d6', tension: 0.3 },
                { label: 'Edit Fills', data: fbData.map(function(d) { return d.edit_fills; }), borderColor: '#2ea44f', borderDash: [5,5], tension: 0.3 },
                { label: 'Mentions', data: fbData.map(function(d) { return d.mentions; }), borderColor: '#e36209', borderDash: [5,5], tension: 0.3 }
            ]
        },
        options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true } } }
    });
}
```

- [ ] **Step 4: Run go vet and build**

Run: `go vet ./... && go build ./cmd/server/`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add cmd/server/template.html
git commit -m "feat: add Trends tab to dashboard with 12-week Chart.js charts"
```

---

## Task 7: Final Validation

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 -short`
Expected: all packages pass

- [ ] **Step 2: Run linter**

Run: `$(go env GOPATH)/bin/golangci-lint run ./...`
Expected: clean

- [ ] **Step 3: Build server binary**

Run: `go build -o /dev/null ./cmd/server/`
Expected: builds successfully

- [ ] **Step 4: Commit any fixes from validation**

If any fixes were needed, commit them.

- [ ] **Step 5: Final commit — update CLAUDE.md**

Add `/report/trends` to the HTTP server entry point line in the project structure:

```bash
git add CLAUDE.md
git commit -m "docs: add /report/trends to CLAUDE.md project structure"
```
