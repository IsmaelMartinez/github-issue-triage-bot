# Dashboard Consolidation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the static GitHub Pages dashboard (`cmd/dashboard`) and simplify the daily cron workflow to only run cleanup, health checks, and reaction sync. Enhance the live `/dashboard` endpoint with time-series charts, drill-down views, auto-refresh, and improved data queries.

**Architecture:** The live Cloud Run `/dashboard` endpoint becomes the single dashboard. The backend gains new time-series query methods that return daily-bucketed data for charts. The frontend template gets Chart.js (CDN) for line/bar charts, clickable rows for drill-down modals, and a 60-second auto-refresh. The static generator (`cmd/dashboard/`) is deleted, the GitHub Actions workflow is simplified, and ADR 011 is superseded by a new ADR.

**Tech Stack:** Go 1.26, html/template, Chart.js 4.x (CDN), pgx/v5, PostgreSQL with pgvector

**Security note:** The drill-down modal constructs content using DOM-safe APIs (`createTextNode`, `element.textContent`) which prevent HTML injection. All data originates from our own database (not user input), and the dashboard is an internal maintainer tool.

---

### Task 1: Add time-series query methods to store

**Files:**
- Modify: `internal/store/report.go`
- Create: `internal/store/report_test.go`

These new methods return daily-bucketed counts for the past 30 days, which the dashboard will use to render charts. Each method returns a slice of `DailyBucket{Date string, Count int}` structs.

- [ ] **Step 1: Write failing tests for DailyTriageCounts**

```go
// internal/store/report_test.go
package store

import "testing"

func TestDailyBucketStructure(t *testing.T) {
	// Verify DailyBucket type exists and has expected fields
	b := DailyBucket{Date: "2026-03-17", Count: 5}
	if b.Date != "2026-03-17" || b.Count != 5 {
		t.Fatalf("unexpected DailyBucket: %+v", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestDailyBucket -v`
Expected: FAIL — `DailyBucket` not defined

- [ ] **Step 3: Add DailyBucket type and time-series query methods**

Add to `internal/store/report.go`:

```go
// DailyBucket represents a single day's count for time-series charts.
type DailyBucket struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GetDailyTriageCounts returns triage session counts per day for the last 30 days.
func (s *Store) GetDailyTriageCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date::text AS date, COALESCE(COUNT(t.id), 0) AS count
		FROM generate_series(now() - interval '30 days', now(), '1 day') AS d
		LEFT JOIN triage_sessions t ON t.repo = $1 AND t.created_at::date = d::date
		GROUP BY d::date ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []DailyBucket
	for rows.Next() {
		var b DailyBucket
		if err := rows.Scan(&b.Date, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// GetDailyAgentCounts returns agent session counts per day for the last 30 days.
func (s *Store) GetDailyAgentCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date::text AS date, COALESCE(COUNT(a.id), 0) AS count
		FROM generate_series(now() - interval '30 days', now(), '1 day') AS d
		LEFT JOIN agent_sessions a ON a.repo = $1 AND a.created_at::date = d::date
		GROUP BY d::date ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []DailyBucket
	for rows.Next() {
		var b DailyBucket
		if err := rows.Scan(&b.Date, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// GetDailyFeedbackCounts returns feedback signal counts per day for the last 30 days.
func (s *Store) GetDailyFeedbackCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date::text AS date, COALESCE(COUNT(f.id), 0) AS count
		FROM generate_series(now() - interval '30 days', now(), '1 day') AS d
		LEFT JOIN feedback_signals f ON f.repo = $1 AND f.created_at::date = d::date
		GROUP BY d::date ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []DailyBucket
	for rows.Next() {
		var b DailyBucket
		if err := rows.Scan(&b.Date, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestDailyBucket -v`
Expected: PASS

- [ ] **Step 5: Add time-series fields to DashboardStats and wire them into GetDashboardStats**

Add three new fields to the `DashboardStats` struct and populate them at the end of `GetDashboardStats`:

```go
// In DashboardStats struct:
DailyTriageCounts   []DailyBucket `json:"daily_triage_counts"`
DailyAgentCounts    []DailyBucket `json:"daily_agent_counts"`
DailyFeedbackCounts []DailyBucket `json:"daily_feedback_counts"`
```

Wire into `GetDashboardStats` (non-fatal if they fail, like feedback):

```go
if dtc, err := s.GetDailyTriageCounts(ctx, repo); err != nil {
	slog.Warn("daily triage counts failed", "error", err)
} else {
	stats.DailyTriageCounts = dtc
}
if dac, err := s.GetDailyAgentCounts(ctx, repo); err != nil {
	slog.Warn("daily agent counts failed", "error", err)
} else {
	stats.DailyAgentCounts = dac
}
if dfc, err := s.GetDailyFeedbackCounts(ctx, repo); err != nil {
	slog.Warn("daily feedback counts failed", "error", err)
} else {
	stats.DailyFeedbackCounts = dfc
}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 7: Run linter**

Run: `golangci-lint run ./...`
Expected: No new issues

- [ ] **Step 8: Commit**

```bash
git add internal/store/report.go internal/store/report_test.go
git commit -m "feat: add time-series query methods for dashboard charts"
```

---

### Task 2: Add detail query methods for drill-down

**Files:**
- Modify: `internal/store/report.go`

The current dashboard shows recent triage and agent sessions capped at 10 rows. For drill-down, we need detail queries that return the full triage comment and audit log.

- [ ] **Step 1: Add GetTriageSessionDetail method**

```go
// TriageDetail holds full triage session details for drill-down.
type TriageDetail struct {
	ID              int64    `json:"id"`
	Repo            string   `json:"repo"`
	IssueNumber     int      `json:"issue_number"`
	ShadowRepo      string   `json:"shadow_repo"`
	ShadowIssue     int      `json:"shadow_issue"`
	TriageComment   string   `json:"triage_comment"`
	PhasesRun       []string `json:"phases_run"`
	Promoted        bool     `json:"promoted"`
	CreatedAt       string   `json:"created_at"`
}

// GetTriageSessionDetail returns a single triage session by issue number.
func (s *Store) GetTriageSessionDetail(ctx context.Context, repo string, issueNumber int) (*TriageDetail, error) {
	td := &TriageDetail{}
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.repo, t.issue_number, t.shadow_repo, t.shadow_issue_number, t.triage_comment, t.phases_run,
			EXISTS(SELECT 1 FROM bot_comments b WHERE b.repo = t.repo AND b.issue_number = t.issue_number),
			t.created_at
		FROM triage_sessions t
		WHERE t.repo = $1 AND t.issue_number = $2
	`, repo, issueNumber).Scan(&td.ID, &td.Repo, &td.IssueNumber, &td.ShadowRepo, &td.ShadowIssue,
		&td.TriageComment, &td.PhasesRun, &td.Promoted, &createdAt)
	if err != nil {
		return nil, err
	}
	td.CreatedAt = createdAt.Format(time.RFC3339)
	return td, nil
}
```

- [ ] **Step 2: Add GetAgentSessionDetail method**

```go
// AgentDetail holds full agent session details for drill-down.
type AgentDetail struct {
	ID              int64           `json:"id"`
	Repo            string          `json:"repo"`
	IssueNumber     int             `json:"issue_number"`
	ShadowRepo      string          `json:"shadow_repo"`
	ShadowIssue     int             `json:"shadow_issue"`
	Stage           string          `json:"stage"`
	RoundTripCount  int             `json:"round_trip_count"`
	AuditLog        []AuditLogEntry `json:"audit_log"`
	CreatedAt       string          `json:"created_at"`
}

// AuditLogEntry represents a single agent audit log entry for the dashboard.
// Named AuditLogEntry to avoid collision with the existing AuditEntry in models.go.
type AuditLogEntry struct {
	ActionType      string  `json:"action_type"`
	OutputSummary   string  `json:"output_summary"`
	SafetyPassed    bool    `json:"safety_passed"`
	ConfidenceScore float64 `json:"confidence_score"`
	CreatedAt       string  `json:"created_at"`
}

// GetAgentSessionDetail returns an agent session with its audit log.
func (s *Store) GetAgentSessionDetail(ctx context.Context, repo string, issueNumber int) (*AgentDetail, error) {
	ad := &AgentDetail{AuditLog: []AuditLogEntry{}}
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, repo, issue_number, shadow_repo, shadow_issue_number, stage, round_trip_count, created_at
		FROM agent_sessions WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber).Scan(&ad.ID, &ad.Repo, &ad.IssueNumber, &ad.ShadowRepo, &ad.ShadowIssue,
		&ad.Stage, &ad.RoundTripCount, &createdAt)
	if err != nil {
		return nil, err
	}
	ad.CreatedAt = createdAt.Format(time.RFC3339)

	rows, err := s.pool.Query(ctx, `
		SELECT action_type, output_summary, safety_check_passed, confidence_score, created_at
		FROM agent_audit_log WHERE session_id = $1 ORDER BY created_at
	`, ad.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ae AuditLogEntry
		var aeCreatedAt time.Time
		if err := rows.Scan(&ae.ActionType, &ae.OutputSummary, &ae.SafetyPassed, &ae.ConfidenceScore, &aeCreatedAt); err != nil {
			return nil, err
		}
		ae.CreatedAt = aeCreatedAt.Format(time.RFC3339)
		ad.AuditLog = append(ad.AuditLog, ae)
	}
	return ad, rows.Err()
}
```

- [ ] **Step 3: Run full test suite and linter**

Run: `go test ./... && golangci-lint run ./...`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: add detail query methods for dashboard drill-down"
```

---

### Task 3: Add detail API endpoints

**Files:**
- Modify: `cmd/server/main.go`

Register two JSON API endpoints that the dashboard frontend will call for drill-down.

- [ ] **Step 1: Add `/api/triage/` and `/api/agent/` endpoints**

Add after the `/report` handler in `main.go`:

```go
mux.HandleFunc("/api/triage/", func(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}
	if !allowedRepos[repo] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	issueStr := strings.TrimPrefix(r.URL.Path, "/api/triage/")
	issueNum, err := strconv.Atoi(issueStr)
	if err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
		return
	}
	detail, err := s.GetTriageSessionDetail(r.Context(), repo, issueNum)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(detail); encErr != nil {
		logger.Error("encoding triage detail", "error", encErr)
	}
})

mux.HandleFunc("/api/agent/", func(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}
	if !allowedRepos[repo] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	issueStr := strings.TrimPrefix(r.URL.Path, "/api/agent/")
	issueNum, err := strconv.Atoi(issueStr)
	if err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
		return
	}
	detail, err := s.GetAgentSessionDetail(r.Context(), repo, issueNum)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if encErr := json.NewEncoder(w).Encode(detail); encErr != nil {
		logger.Error("encoding agent detail", "error", encErr)
	}
})
```

- [ ] **Step 2: Run full test suite and linter**

Run: `go test ./... && golangci-lint run ./...`
Expected: All pass

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: add triage and agent detail API endpoints for drill-down"
```

---

### Task 4: Rewrite the dashboard template

**Files:**
- Modify: `cmd/server/template.html`

This is the largest task. The template gets Chart.js for time-series charts, clickable rows with a detail modal, and auto-refresh. The existing sidebar layout and health cards are kept. The key additions are:

1. Chart.js CDN script tag in `<head>`
2. Activity trend chart (30 days) on the Overview page using `daily_triage_counts` and `daily_agent_counts`
3. Feedback trend chart on the Triage page using `daily_feedback_counts`
4. Clickable table rows that fetch `/api/triage/{issue}` or `/api/agent/{issue}` and display a detail modal (content sanitized via `escHtml()` which uses `textContent` for safe escaping)
5. A 60-second auto-refresh via `setTimeout` that reloads the page
6. Increase recent session limits from 10 to 25

- [ ] **Step 1: Add Chart.js CDN and auto-refresh**

In the `<head>` section, after the `<style>` block, add:

```html
<script src="https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js"></script>
```

At the end of the `<script>` block, add:

```javascript
/* Auto-refresh every 60 seconds */
setTimeout(function() { location.reload(); }, 60000);
```

- [ ] **Step 2: Add modal CSS and HTML**

Add to the CSS:

```css
.modal-overlay { display: none; position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 100; justify-content: center; align-items: center; }
.modal-overlay.open { display: flex; }
.modal { background: #fff; border-radius: 10px; padding: 20px; max-width: 700px; width: 90%; max-height: 80vh; overflow-y: auto; position: relative; }
.modal-close { position: absolute; top: 10px; right: 14px; background: none; border: none; font-size: 1.2rem; cursor: pointer; color: #555; }
.modal h3 { margin-bottom: 12px; }
.modal pre { background: #f6f8fa; padding: 12px; border-radius: 6px; font-size: 0.8rem; white-space: pre-wrap; word-break: break-word; max-height: 300px; overflow-y: auto; }
.audit-entry { border-left: 3px solid #e1e4e8; padding: 8px 12px; margin-bottom: 8px; font-size: 0.82rem; }
.audit-entry.passed { border-left-color: #2ea44f; }
.audit-entry.failed { border-left-color: #da3633; }
tr.clickable { cursor: pointer; } tr.clickable:hover { background: #f6f8fa; }
```

Add before closing `</body>`:

```html
<div class="modal-overlay" id="modal-overlay">
    <div class="modal">
        <button class="modal-close" onclick="closeModal()">&times;</button>
        <div id="modal-content"></div>
    </div>
</div>
```

- [ ] **Step 3: Add chart containers to the Overview and Triage views**

In the Overview view (`view-overview`), after the health cards div and before the two-col div, add:

```html
<div class="panel"><h3>Activity (30 days)</h3><canvas id="activity-chart" height="180"></canvas></div>
```

In the Triage view (`view-triage`), after the phase bars panel, add:

```html
<div class="panel"><h3>Feedback Trend (30 days)</h3><canvas id="feedback-chart" height="150"></canvas></div>
```

- [ ] **Step 4: Add JavaScript for charts**

After the existing data setup in the `<script>` block, add:

```javascript
/* Charts */
var dtc = stats.daily_triage_counts || [];
var dac = stats.daily_agent_counts || [];
var dfc = stats.daily_feedback_counts || [];

if (dtc.length > 0 && typeof Chart !== 'undefined') {
    new Chart(document.getElementById('activity-chart'), {
        type: 'line',
        data: {
            labels: dtc.map(function(d) { return d.date.substring(5); }),
            datasets: [
                { label: 'Triage', data: dtc.map(function(d) { return d.count; }), borderColor: '#2ea44f', backgroundColor: 'rgba(46,164,79,0.1)', fill: true, tension: 0.3 },
                { label: 'Agent', data: dac.map(function(d) { return d.count; }), borderColor: '#0366d6', backgroundColor: 'rgba(3,102,214,0.1)', fill: true, tension: 0.3 }
            ]
        },
        options: { responsive: true, plugins: { legend: { position: 'bottom' } }, scales: { y: { beginAtZero: true, ticks: { stepSize: 1 } } } }
    });
}

if (dfc.length > 0 && typeof Chart !== 'undefined') {
    new Chart(document.getElementById('feedback-chart'), {
        type: 'bar',
        data: {
            labels: dfc.map(function(d) { return d.date.substring(5); }),
            datasets: [{ label: 'Feedback signals', data: dfc.map(function(d) { return d.count; }), backgroundColor: '#d29922' }]
        },
        options: { responsive: true, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true, ticks: { stepSize: 1 } } } }
    });
}
```

- [ ] **Step 5: Add JavaScript for modal drill-down**

All content inserted into the modal is sanitized via `escHtml()`, which uses `document.createElement('div').textContent` to safely escape HTML entities. The modal is built using safe DOM construction.

```javascript
/* Modal drill-down */
function closeModal() { document.getElementById('modal-overlay').classList.remove('open'); }
document.getElementById('modal-overlay').addEventListener('click', function(e) { if (e.target === this) closeModal(); });
function escHtml(s) { var d = document.createElement('div'); d.textContent = s || ''; return d.innerHTML; }

function showTriageDetail(repo, issue) {
    fetch('/api/triage/' + issue + '?repo=' + encodeURIComponent(repo))
        .then(function(r) { return r.json(); })
        .then(function(d) {
            var mc = document.getElementById('modal-content');
            // Clear existing content
            mc.textContent = '';
            // Build modal with safe DOM methods
            var h = el('h3', {text: 'Triage: #' + d.issue_number});
            var meta = el('p');
            meta.appendChild(el('strong', {text: 'Repo: '}));
            meta.appendChild(document.createTextNode(d.repo + ' | '));
            meta.appendChild(el('strong', {text: 'Shadow: '}));
            meta.appendChild(document.createTextNode(d.shadow_repo + '#' + d.shadow_issue + ' | '));
            meta.appendChild(el('strong', {text: 'Status: '}));
            meta.appendChild(document.createTextNode(d.promoted ? 'Promoted' : 'Pending'));
            meta.appendChild(document.createTextNode(' | '));
            meta.appendChild(el('strong', {text: 'Date: '}));
            meta.appendChild(document.createTextNode(fDate(d.created_at)));
            var phases = el('p');
            phases.appendChild(el('strong', {text: 'Phases: '}));
            phases.appendChild(document.createTextNode((d.phases_run || []).join(', ')));
            var commentH = el('h4', {text: 'Triage Comment'});
            var pre = el('pre', {text: d.triage_comment || ''});
            mc.appendChild(h);
            mc.appendChild(meta);
            mc.appendChild(phases);
            mc.appendChild(commentH);
            mc.appendChild(pre);
            document.getElementById('modal-overlay').classList.add('open');
        });
}

function showAgentDetail(repo, issue) {
    fetch('/api/agent/' + issue + '?repo=' + encodeURIComponent(repo))
        .then(function(r) { return r.json(); })
        .then(function(d) {
            var mc = document.getElementById('modal-content');
            mc.textContent = '';
            var h = el('h3', {text: 'Agent: #' + d.issue_number});
            var meta = el('p');
            meta.appendChild(el('strong', {text: 'Repo: '}));
            meta.appendChild(document.createTextNode(d.repo + ' | '));
            meta.appendChild(el('strong', {text: 'Shadow: '}));
            meta.appendChild(document.createTextNode(d.shadow_repo + '#' + d.shadow_issue + ' | '));
            meta.appendChild(el('strong', {text: 'Stage: '}));
            meta.appendChild(document.createTextNode(d.stage + ' | '));
            meta.appendChild(el('strong', {text: 'Round trips: '}));
            meta.appendChild(document.createTextNode(d.round_trip_count));
            meta.appendChild(document.createTextNode(' | '));
            meta.appendChild(el('strong', {text: 'Date: '}));
            meta.appendChild(document.createTextNode(fDate(d.created_at)));
            var logH = el('h4', {text: 'Audit Log'});
            mc.appendChild(h);
            mc.appendChild(meta);
            mc.appendChild(logH);
            (d.audit_log || []).forEach(function(ae) {
                var entry = el('div', {cls: 'audit-entry ' + (ae.safety_passed ? 'passed' : 'failed')});
                entry.appendChild(el('strong', {text: ae.action_type}));
                entry.appendChild(document.createTextNode(' \u2014 ' + fDate(ae.created_at) + ' (confidence: ' + (ae.confidence_score * 100).toFixed(0) + '%, safety: ' + (ae.safety_passed ? 'pass' : 'fail') + ')'));
                entry.appendChild(el('br'));
                entry.appendChild(document.createTextNode((ae.output_summary || '').substring(0, 500)));
                mc.appendChild(entry);
            });
            document.getElementById('modal-overlay').classList.add('open');
        });
}
```

- [ ] **Step 6: Make triage and agent table rows clickable**

Replace the existing `fillTbl('triage-table', ...)` and `fillTbl('agent-table', ...)` calls with custom loops that add click handlers and the `clickable` class:

```javascript
// Replace the existing fillTbl('triage-table', ...) call with:
(ts.recent || []).forEach(function(t) {
    var tr = el('tr', {cls: 'clickable'});
    tr.onclick = function() { showTriageDetail(t.repo, t.issue_number); };
    var cells = [iLink(t.repo, t.issue_number), t.shadow_repo ? iLink(t.shadow_repo, t.shadow_issue) : el('span', {text: '#' + t.shadow_issue}), t.promoted ? mkTag('promoted', 'tag-promoted') : mkTag('pending', 'tag-pending'), fDate(t.created_at)];
    cells.forEach(function(c) { var td = el('td'); if (typeof c === 'string') td.textContent = c; else td.appendChild(c); tr.appendChild(td); });
    document.querySelector('#triage-table tbody').appendChild(tr);
});

// Replace the existing fillTbl('agent-table', ...) call with:
(ag.recent || []).forEach(function(a) {
    var tr = el('tr', {cls: 'clickable'});
    tr.onclick = function() { showAgentDetail(a.repo, a.issue_number); };
    var cells = [iLink(a.repo, a.issue_number), a.shadow_repo ? iLink(a.shadow_repo, a.shadow_issue) : el('span', {text: '#' + a.shadow_issue}), stageTag(a.stage), fDate(a.created_at)];
    cells.forEach(function(c) { var td = el('td'); if (typeof c === 'string') td.textContent = c; else td.appendChild(c); tr.appendChild(td); });
    document.querySelector('#agent-table tbody').appendChild(tr);
});
```

- [ ] **Step 7: Run full test suite and vet**

Run: `go test ./... && go vet ./...`
Expected: All pass (template changes don't affect Go tests, but go vet catches embed issues)

- [ ] **Step 8: Commit**

```bash
git add cmd/server/template.html
git commit -m "feat: add charts, drill-down modal, and auto-refresh to live dashboard"
```

---

### Task 5: Increase recent session limits

**Files:**
- Modify: `internal/store/report.go`

The current queries cap recent sessions at 10 (triage) and 20 (comments). Increase these to 25 so the dashboard shows more history.

- [ ] **Step 1: Update LIMIT clauses**

In `getTriageStats`: change `LIMIT 10` to `LIMIT 25`.
In `getAgentStats`: change `LIMIT 10` to `LIMIT 25`.
In `GetDashboardStats` (recent comments): change `LIMIT 20` to `LIMIT 25`.

- [ ] **Step 2: Run tests and linter**

Run: `go test ./... && golangci-lint run ./...`
Expected: All pass

- [ ] **Step 3: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: increase recent session limits to 25 in dashboard queries"
```

---

### Task 6: Delete static dashboard generator

**Files:**
- Delete: `cmd/dashboard/main.go`
- Modify: `.github/workflows/dashboard.yml`
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Delete cmd/dashboard/**

```bash
rm cmd/dashboard/main.go
rmdir cmd/dashboard
```

- [ ] **Step 2: Rewrite the dashboard.yml workflow**

Remove the `generate` job entirely. Keep the `cleanup` job (stale issue closing + health check) and move reaction sync into it. The workflow becomes a single `maintenance` job.

Drop `pages: write` permission (no longer deploying to GitHub Pages). Change `contents: write` to `contents: read`. Rename the workflow from "Update Dashboard" to "Daily Maintenance".

The merged `maintenance` job must include `actions/checkout` and `actions/setup-go` steps (currently only in the `generate` job) since `cmd/sync-reactions` requires a Go build. The key structural changes: merge both jobs into one, remove the `go run ./cmd/dashboard` and `peaceiris/actions-gh-pages` steps, drop `pages: write` permission, change `contents: write` to `contents: read`.

- [ ] **Step 3: Update CLAUDE.md**

Remove: `go build -o dashboard ./cmd/dashboard` from Essential Commands.
Remove: `go run ./cmd/dashboard [output-path]` from Essential Commands.
Remove: `cmd/dashboard/main.go` line from Project Structure.
Change dashboard workflow description from "Daily dashboard generation + GitHub Pages + stale cleanup + health check" to "Daily maintenance: stale cleanup, health check, reaction sync".

- [ ] **Step 4: Update README.md**

Remove the GitHub Pages link from the Dashboard section. Remove `(daily via cmd/dashboard)` from the architecture diagram. The Dashboard section should only reference the live endpoint at `https://triage-bot-lhuutxzbnq-uc.a.run.app/dashboard`.

- [ ] **Step 5: Run full test suite and linter**

Run: `go test ./... && golangci-lint run ./...`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: remove static dashboard generator, simplify daily workflow"
```

---

### Task 7: Supersede ADR 011

**Files:**
- Modify: `docs/adr/011-public-dashboard.md`
- Create: `docs/adr/012-dashboard-consolidation.md`

- [ ] **Step 1: Mark ADR 011 as superseded**

Change the Status line in `docs/adr/011-public-dashboard.md` from `Implemented` to `Superseded by ADR 012`.

- [ ] **Step 2: Write ADR 012**

Create `docs/adr/012-dashboard-consolidation.md` documenting the decision to consolidate on the live endpoint, the reasons (duplicate maintenance, identical output, the live endpoint now having charts and drill-down that the static version could never support), and the consequences (no offline fallback, simpler workflow, single template to maintain).

- [ ] **Step 3: Commit**

```bash
git add docs/adr/011-public-dashboard.md docs/adr/012-dashboard-consolidation.md
git commit -m "docs: add ADR 012 (dashboard consolidation), supersede ADR 011"
```

---

### Task 8: Clean up stale worktrees

**Files:**
- None (shell commands only)

- [ ] **Step 1: List and remove stale worktrees**

```bash
git worktree list
git worktree prune
rm -rf .claude/worktrees/abundant-juggling-diffie .claude/worktrees/agent-a6403d7a .claude/worktrees/agent-a8d15e21
git worktree prune
```

- [ ] **Step 2: Verify clean state**

```bash
git worktree list
go test ./...
golangci-lint run ./...
```

---

### Task 9: Final verification

- [ ] **Step 1: Build all binaries**

```bash
go build -o server ./cmd/server
go build -o seed ./cmd/seed
go build -o sync-reactions ./cmd/sync-reactions
```

Verify that `go build ./cmd/dashboard` fails (directory deleted).

- [ ] **Step 2: Run full test suite**

```bash
go test ./...
```

- [ ] **Step 3: Run linter**

```bash
golangci-lint run ./...
```

- [ ] **Step 4: Run vet**

```bash
go vet ./...
```

- [ ] **Step 5: Create PR**

```bash
git push -u origin <branch>
gh pr create --title "feat: consolidate dashboards — live endpoint with charts and drill-down" --body "..."
```
