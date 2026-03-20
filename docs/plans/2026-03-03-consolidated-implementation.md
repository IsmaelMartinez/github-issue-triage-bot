> **IMPLEMENTED**: All 36 tasks across 9 batches are complete. See the Repository Strategist implementation plan for current work.

# Consolidated Implementation Plan

**Goal:** Harden the triage bot's security, seed the full issue dataset, add a public dashboard, convert to a GitHub App, and cut over to production on teams-for-linux — validating everything iteratively on triage-bot-test-repo.

**Architecture:** Nine batches (0-8) executed sequentially. Each batch is a PR against main. The `test` job must pass before merge. The `deploy` job runs on push to main.

**Tech Stack:** Go 1.26, Terraform >= 1.5, GCS, Google Secret Manager, GitHub Actions, Gemini 2.5 Flash + gemini-embedding-001, Neon PostgreSQL + pgvector, Cloud Run v2, bradleyfalzon/ghinstallation.

**Workflow:** Create a branch per batch, make changes, push, open PR, wait for `test` job to pass, merge. Deploy triggers automatically on merge to main. Validate on triage-bot-test-repo after each deploy.

---

## Batch 0: Baseline Validation [DONE]

Completed 2026-03-03. Test issues #10-#14 created on triage-bot-test-repo. Results in `docs/validation/batch0-baseline.md`.

## Batch 1: Quick Security Fixes [DONE]

Completed 2026-03-03. Six commits merged to main:
- `1b99695` security: move Gemini API key from URL to request header
- `66ba5e9` security: add 25 MB body size limit to webhook handler
- `15c21a7` security: run Docker container as non-root user
- `aad084b` security: cap error response body reads to 4 KB
- `f0f096a` fix: replace DDL AfterConnect hook with pgvector.RegisterTypes
- `4c953b7` fix: validate embedding dimensions before database insert

Validated on test repo (issue #15).

---

## Batch 2: Secret Manager Migration [DONE]

Completed 2026-03-03. PR #2 merged to main. Terraform changes to use Google Secret Manager for all sensitive env vars.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch2-secret-manager` and make these Terraform changes in `terraform/main.tf`:

1. Enable the Secret Manager API:
   resource "google_project_service" "secretmanager" {
     service            = "secretmanager.googleapis.com"
     disable_on_destroy = false
   }

2. Create a service account for Cloud Run:
   resource "google_service_account" "triage_bot" {
     account_id   = "triage-bot-run"
     display_name = "Triage Bot Cloud Run"
   }

3. For each of these four secrets (database_url, gemini_api_key, github_token, webhook_secret), create three resources:
   - google_secret_manager_secret (secret_id = "triage-bot-<name>", replication { auto {} }, depends_on secretmanager API)
   - google_secret_manager_secret_version (secret_data = var.<name>)
   - google_secret_manager_secret_iam_member (role = "roles/secretmanager.secretAccessor", member = serviceAccount:triage_bot.email)

4. In the google_cloud_run_v2_service resource:
   - Add `service_account = google_service_account.triage_bot.email` in the template block
   - Replace the four plaintext env blocks for DATABASE_URL, GEMINI_API_KEY, GITHUB_TOKEN, WEBHOOK_SECRET with value_source.secret_key_ref references:
     env {
       name = "DATABASE_URL"
       value_source {
         secret_key_ref {
           secret  = google_secret_manager_secret.database_url.secret_id
           version = "latest"
         }
       }
     }
   - Keep SOURCE_REPO as a plain env var (it's not a secret)

5. Remove the default value from the billing_account_id variable (line 60). The value should come from terraform.tfvars only.

Do NOT run terraform apply. Just make the code changes, run `go vet ./...` and `go test ./...` to verify the Go code still compiles, commit with message "security: migrate Cloud Run secrets to Secret Manager", and push the branch.
```

### Manual steps after merge
```bash
cd terraform && terraform plan && terraform apply
```
Then create a test issue on triage-bot-test-repo to verify the bot still works.

---

## Batch 3: Prompt Injection Defenses [DONE]

Completed 2026-03-03. PR #1 merged to main. Added systemInstruction support, migrated all phases, added LLM output sanitization. Follow-up PR #3 added URL sanitization for DocURL/Title fields in the comment builder.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch3-prompt-injection` and make these changes:

TASK 1: Add systemInstruction support to Gemini client

In `internal/llm/client.go`:

1. Add a new request struct alongside the existing geminiRequest:
   type geminiRequestWithSystem struct {
       SystemInstruction *content         `json:"systemInstruction,omitempty"`
       Contents          []content        `json:"contents"`
       GenerationConfig  generationConfig `json:"generationConfig"`
   }

2. Add a new method `GenerateJSONWithSystem(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error)` that:
   - Uses geminiRequestWithSystem with SystemInstruction set to a content{Parts: []part{{Text: systemPrompt}}}
   - The rest is identical to GenerateJSON (same URL, same headers, same response parsing)
   - Uses the x-goog-api-key header (already in place from Batch 1)
   - Uses io.LimitReader on error paths (already in place from Batch 1)

Commit: "feat: add GenerateJSONWithSystem for prompt injection defense"

TASK 2: Migrate all phases to use system instructions

In each phase file, split the current single prompt into a systemPrompt (trusted instructions, JSON format spec) and userContent (untrusted issue data). Replace l.GenerateJSON(ctx, prompt, ...) with l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, ...).

For `internal/phases/phase2.go`:
- systemPrompt = the role ("You are a helpful assistant..."), matching rules, JSON format spec, "Respond with ONLY valid JSON"
- userContent = "KNOWN ISSUES:\n" + summaries + "\n\nBUG REPORT:\nTitle: " + title + "\nBody: " + body

For `internal/phases/phase3.go`:
- systemPrompt = the role, matching rules, similarity percentage instructions, JSON format spec
- userContent = "EXISTING ISSUES:\n" + summaries + "\n\nNEW ISSUE:\nTitle: " + title + "\nBody: " + body

For `internal/phases/phase4a.go`:
- systemPrompt = the role, matching rules, is_infeasible logic, JSON format spec
- userContent = "EXISTING FEATURES/DECISIONS/RESEARCH:\n" + summaries + "\n\nENHANCEMENT REQUEST:\nTitle: " + title + "\nBody: " + body

For `internal/phases/phase4b.go`:
- systemPrompt = the role, classification rules, JSON format spec
- userContent = "ISSUE:\nTitle: " + title + "\nBody: " + body + "\n\nCurrent label: " + currentLabel

Commit: "security: separate system instructions from user content in LLM prompts"

TASK 3: Add LLM output sanitization

Create `internal/comment/sanitize.go`:
   package comment

   import "regexp"

   var (
       dangerousLinkRe = regexp.MustCompile(`\[([^\]]*)\]\((javascript|data|vbscript):[^)]*\)`)
       htmlTagRe       = regexp.MustCompile(`<[^>]*>`)
   )

   func sanitizeLLMOutput(s string) string {
       s = dangerousLinkRe.ReplaceAllString(s, "[$1](removed)")
       s = htmlTagRe.ReplaceAllString(s, "")
       return s
   }

Create `internal/comment/sanitize_test.go` with table-driven tests:
- "plain text" passes through unchanged
- "[click](javascript:alert(1))" becomes "[click](removed)"
- "[x](data:text/html,<script>)" becomes "[x](removed)"
- "[docs](https://example.com)" stays unchanged
- "text <script>alert(1)</script> more" becomes "text  more"
- "**bold** and `code`" stays unchanged

In `internal/comment/builder.go`, wrap all LLM-generated fields with sanitizeLLMOutput():
- Phase 2 (line ~59): s.Reason and s.ActionableStep
- Phase 3 (line ~74 and ~86): d.Reason
- Phase 4a (lines ~124-128): ctx.Reason
- Phase 4b (line ~174): r.Phase4b.Reason

Commit: "security: sanitize LLM output before posting GitHub comments"

Run `go vet ./...` and `go test ./...` after all changes. Push the branch.
```

---

## Batch 4: Webhook Replay Protection and CI Hardening [DONE]

Completed 2026-03-03. PR #4 merged. Migration 002 applied. Three commits: webhook_deliveries table, replay protection in store/handler, GitHub Actions pinned to SHAs.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch4-replay-protection` and make these changes:

TASK 1: Add webhook_deliveries migration

Create `migrations/002_webhook_deliveries.sql`:
   CREATE TABLE IF NOT EXISTS webhook_deliveries (
       delivery_id TEXT PRIMARY KEY,
       created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries (created_at);

Commit: "db: add webhook_deliveries table for replay protection"

TASK 2: Add delivery ID checking to store and handler

In `internal/store/postgres.go`, add method:
   func (s *Store) CheckAndRecordDelivery(ctx context.Context, deliveryID string) (bool, error) {
       var exists bool
       err := s.pool.QueryRow(ctx, `
           WITH ins AS (
               INSERT INTO webhook_deliveries (delivery_id) VALUES ($1)
               ON CONFLICT (delivery_id) DO NOTHING
               RETURNING delivery_id
           )
           SELECT NOT EXISTS(SELECT 1 FROM ins)
       `, deliveryID).Scan(&exists)
       return exists, err
   }

In `internal/webhook/handler.go`, after signature verification (after line ~60) and before the event type check, add:
   deliveryID := r.Header.Get("X-GitHub-Delivery")
   if deliveryID != "" {
       duplicate, err := h.store.CheckAndRecordDelivery(r.Context(), deliveryID)
       if err != nil {
           h.logger.Error("checking delivery ID", "error", err)
       } else if duplicate {
           h.logger.Info("duplicate delivery rejected", "deliveryID", deliveryID)
           w.WriteHeader(http.StatusOK)
           fmt.Fprint(w, "duplicate delivery")
           return
       }
   }

Commit: "security: add webhook replay protection via delivery ID tracking"

TASK 3: Pin GitHub Actions to commit SHAs

In `.github/workflows/deploy.yml`, look up the current commit SHA for each action tag used and replace them. Use `gh api repos/OWNER/REPO/git/ref/tags/TAG` or check the action repos directly. Pin with a version comment:

   - uses: actions/checkout@<sha>  # v4
   - uses: actions/setup-go@<sha>  # v5
   - uses: google-github-actions/auth@<sha>  # v2
   - uses: google-github-actions/setup-gcloud@<sha>  # v2

Commit: "security: pin GitHub Actions to commit SHAs"

Run `go vet ./...` and `go test ./...`. Push the branch.
```

### Manual steps after merge
Apply the migration:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -f migrations/002_webhook_deliveries.sql
```

---

## Batch 5: Data Seeding [DONE]

Completed 2026-03-03. PR #5 merged. All 1,356 issues from teams-for-linux exported and seeded with embeddings. Migrations 003 applied. Four commits: export tool, seed rate limiting, ivfflat migration, closed_at parsing fix.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch5-data-seeding` and make these changes:

TASK 1: Create issue export tool

Create `cmd/export-issues/main.go` — a CLI that:
- Reads GITHUB_TOKEN from env (required)
- Defaults to repo "IsmaelMartinez/teams-for-linux", overridable via first CLI arg
- Paginates through all issues via GitHub API (GET /repos/{repo}/issues?state=all&per_page=100&page={n}&direction=asc)
- Filters out pull requests (skip entries where pull_request field is non-null)
- For each issue, outputs JSON: {number, title, state, labels (array of label name strings), summary (body stripped of code fences and HTML, truncated to 500 chars), created_at, closed_at, milestone (title string or null)}
- Outputs the full JSON array to stdout, progress to stderr
- Sleeps 500ms between pages for rate limiting
- Uses net/http directly, no external dependencies

Commit: "feat: add issue export tool for bulk seeding"

TASK 2: Add rate limiting to seed CLI

In `cmd/seed/main.go`, add "time" to imports. In all three seed functions (seedIssues, seedTroubleshooting, seedFeatures), add after the logger.Info line at the end of each loop iteration:
   if (i+1)%50 == 0 {
       logger.Info("rate limit pause", "processed", i+1, "remaining", len(entries)-i-1)
       time.Sleep(3 * time.Second)
   }

Commit: "feat: add rate limiting to seed CLI"

TASK 3: Create ivfflat migration

Create `migrations/003_update_ivfflat_lists.sql`:
   DROP INDEX IF EXISTS idx_issues_embedding;
   CREATE INDEX idx_issues_embedding ON issues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 40);
   DROP INDEX IF EXISTS idx_documents_embedding;
   CREATE INDEX idx_documents_embedding ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 25);

Commit: "db: update ivfflat lists for larger dataset"

Run `go vet ./...` and `go test ./...`. Push the branch.
```

### Manual steps after merge
```bash
# Export all issues
GITHUB_TOKEN=<token> go run ./cmd/export-issues > /tmp/all-issues.json

# Seed issues
DATABASE_URL=<url> GEMINI_API_KEY=<key> go run ./cmd/seed issues /tmp/all-issues.json

# Seed features (if feature-index.json exists)
DATABASE_URL=<url> GEMINI_API_KEY=<key> go run ./cmd/seed features /path/to/feature-index.json

# Apply ivfflat migration
psql "$DATABASE_URL" -f migrations/003_update_ivfflat_lists.sql

# Remove the export tool (one-time utility)
rm -rf cmd/export-issues
git add -A cmd/export-issues && git commit -m "chore: remove export-issues tool"
```

---

## Batch 6: Dashboard [DONE]

Completed 2026-03-03. PR #6 merged. Added dashboard store methods, /report endpoint, static HTML generator, reaction sync tool, and daily dashboard workflow. Review fixes: pinned GH Actions SHAs, fixed createdAt scan, removed unused method, initialized RecentComments slice.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch6-dashboard` and make these changes:

TASK 1: Add dashboard store methods

Create `internal/store/report.go` with:

type DashboardStats struct {
    TotalComments  int            `json:"total_comments"`
    TotalThumbsUp  int            `json:"total_thumbs_up"`
    TotalThumbsDown int           `json:"total_thumbs_down"`
    PhaseBreakdown map[string]int `json:"phase_breakdown"`
    DocumentCounts map[string]int `json:"document_counts"`
    IssueCount     int            `json:"issue_count"`
    RecentComments []RecentComment `json:"recent_comments"`
}

type RecentComment struct {
    Repo        string   `json:"repo"`
    IssueNumber int      `json:"issue_number"`
    CommentID   int64    `json:"comment_id"`
    PhasesRun   []string `json:"phases_run"`
    ThumbsUp    int      `json:"thumbs_up"`
    ThumbsDown  int      `json:"thumbs_down"`
    CreatedAt   string   `json:"created_at"`
}

func (s *Store) GetDashboardStats(ctx context.Context, repo string) (*DashboardStats, error)
- Query total comments, sum thumbs_up, sum thumbs_down from bot_comments WHERE repo = $1
- Query phase breakdown: SELECT phase, count(*) FROM bot_comments, unnest(phases_run) AS phase WHERE repo = $1 GROUP BY phase
- Query document counts: SELECT doc_type, count(*) FROM documents WHERE repo = $1 GROUP BY doc_type
- Query issue count from issues WHERE repo = $1
- Query recent 20 comments ordered by created_at DESC

func (s *Store) UpdateReactions(ctx context.Context, repo string, issueNumber, thumbsUp, thumbsDown int) error
- UPDATE bot_comments SET thumbs_up = $3, thumbs_down = $4 WHERE repo = $1 AND issue_number = $2

func (s *Store) ListBotComments(ctx context.Context, repo string) ([]BotComment, error)
- SELECT all columns from bot_comments WHERE repo = $1

Commit: "feat: add dashboard stats query methods"

TASK 2: Add /report endpoint

In `cmd/server/main.go`, add "encoding/json" to imports. After the /health handler, add:
   mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
       repo := r.URL.Query().Get("repo")
       if repo == "" { repo = "IsmaelMartinez/teams-for-linux" }
       stats, err := s.GetDashboardStats(r.Context(), repo)
       if err != nil {
           http.Error(w, "failed to get stats", http.StatusInternalServerError)
           return
       }
       w.Header().Set("Content-Type", "application/json")
       w.Header().Set("Access-Control-Allow-Origin", "*")
       json.NewEncoder(w).Encode(stats)
   })

Commit: "feat: add /report JSON endpoint for dashboard"

TASK 3: Create static dashboard generator

Create `cmd/dashboard/main.go` — connects to DB via DATABASE_URL, calls GetDashboardStats, renders an HTML template to a file (default docs/dashboard/index.html). Uses Go html/template with embedded JSON data.

Create `cmd/dashboard/template.html` — a single self-contained HTML file with:
- Embedded CSS (no external deps), clean design
- Header with repo name and generation timestamp
- Stat cards: total comments, thumbs up/down ratio, documents indexed, issues indexed
- Phase breakdown table
- Recent comments table with links to GitHub issues (https://github.com/{repo}/issues/{number})
- The data comes from a <script> tag with: const stats = {{.StatsJSON}};

Commit: "feat: add static dashboard HTML generator"

TASK 4: Create reaction sync tool

Create `cmd/sync-reactions/main.go` — connects to DB and GitHub API, lists all bot comments, calls GitHub API for reactions on each (GET /repos/{repo}/issues/comments/{comment_id}/reactions), updates thumbs_up/thumbs_down via UpdateReactions. Rate limits with 500ms sleep.

Commit: "feat: add reaction sync tool"

TASK 5: Add dashboard workflow

Create `.github/workflows/dashboard.yml`:
   name: Update Dashboard
   on:
     schedule:
       - cron: '0 6 * * *'
     workflow_dispatch:
   permissions:
     contents: write
     pages: write
   jobs:
     generate:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with:
             go-version-file: go.mod
         - name: Sync reactions
           env:
             DATABASE_URL: ${{ secrets.DATABASE_URL }}
             GITHUB_TOKEN: ${{ secrets.BOT_GITHUB_TOKEN }}
           run: go run ./cmd/sync-reactions
         - name: Generate dashboard
           env:
             DATABASE_URL: ${{ secrets.DATABASE_URL }}
           run: |
             mkdir -p docs/dashboard
             go run ./cmd/dashboard docs/dashboard/index.html
         - name: Deploy to GitHub Pages
           uses: peaceiris/actions-gh-pages@v4
           with:
             github_token: ${{ secrets.GITHUB_TOKEN }}
             publish_dir: ./docs/dashboard

Commit: "ci: add daily dashboard generation workflow"

Run `go vet ./...` and `go test ./...`. Push the branch.
```

---

## Batch 7: GitHub App Conversion [DONE]

Completed 2026-03-03. PR #7 merged. Added ghinstallation dependency, refactored GitHub client for App auth, threaded installation ID through handler, updated server main and Terraform. Review fixes: marked ghinstallation as direct dep, updated docker-compose.yml and CLAUDE.md.

### Codex prompt

```
You are working on the github-issue-triage-bot Go project. Create a branch `batch7-github-app`.

Read ADR 006 at docs/adr/006-github-app-integration.md for full context.

TASK 1: Add ghinstallation dependency

Run: go get github.com/bradleyfalzon/ghinstallation/v2
Commit: "deps: add ghinstallation for GitHub App auth"

TASK 2: Refactor GitHub client for App authentication

In `internal/github/client.go`:

Replace the Client struct to hold App credentials instead of a PAT:
   type Client struct {
       appID      int64
       privateKey []byte
       httpClient *http.Client
       baseURL    string
   }

Replace New() to accept appID and privateKey:
   func New(appID int64, privateKey []byte) *Client

Add a method to get an installation-scoped HTTP client:
   func (c *Client) installationClient(installationID int64) (*http.Client, error)
   - Uses ghinstallation.New(http.DefaultTransport, c.appID, installationID, c.privateKey)

Update CreateComment and ListComments to accept installationID as a parameter and use installationClient() for auth instead of the Bearer token header.

Keep VerifyWebhookSignature unchanged (it doesn't use auth).

Commit: "feat: refactor GitHub client for App authentication"

TASK 3: Update webhook handler for installation ID

In `internal/github/client.go`, add InstallationID to IssueEvent:
   type IssueEvent struct {
       Action       string           `json:"action"`
       Issue        IssueDetail      `json:"issue"`
       Repo         RepoDetail       `json:"repository"`
       Installation InstallationInfo `json:"installation"`
   }
   type InstallationInfo struct {
       ID int64 `json:"id"`
   }

In `internal/webhook/handler.go`, pass event.Installation.ID to CreateComment:
   commentID, err := h.github.CreateComment(ctx, repo, issue.Number, body, event.Installation.ID)

(You'll need to thread the installationID through processEvent and handleOpened.)

Commit: "feat: pass installation ID through webhook handler"

TASK 4: Update server main for App configuration

In `cmd/server/main.go`:
- Replace GITHUB_TOKEN with GITHUB_APP_ID (parse as int64) and GITHUB_PRIVATE_KEY (base64-encoded or raw PEM)
- Update gh.New() call: gh.New(appID, privateKeyBytes)

Commit: "feat: configure server for GitHub App credentials"

TASK 5: Update Terraform for new secrets

In `terraform/main.tf`:
- Replace the github_token secret with github_private_key
- Add GITHUB_APP_ID as a plain env var
- Update variables: replace github_token with github_app_id (string) and github_private_key (sensitive string)

Commit: "infra: replace GitHub token with App credentials in Terraform"

Run `go vet ./...` and `go test ./...`. Push the branch.
```

### Manual steps after merge
1. Register the GitHub App at https://github.com/settings/apps/new with Issues read/write, subscribe to issues event, webhook URL = Cloud Run URL
2. Download the private key PEM file
3. Add to terraform.tfvars: github_app_id and github_private_key
4. `cd terraform && terraform apply`
5. Install the App on triage-bot-test-repo
6. Remove old PAT webhook from test repo
7. Create a test issue, verify bot responds with App identity

---

## Batch 8: Production Cutover [DONE]

Completed 2026-03-03. PR #8. Updated remaining work doc (all items marked complete), updated CLAUDE.md project structure and dependencies, updated README.md with GitHub App installation instructions.

### Codex prompt

Not needed — this is operational. Steps:

1. Install the GitHub App on IsmaelMartinez/teams-for-linux
2. Create a test issue, verify bot responds correctly
3. Close and delete the test issue
4. Disable old triage bot workflows in teams-for-linux (keep one week)
5. Update docs:

```bash
git checkout -b batch8-cutover
# Update docs/decisions/001-remaining-work.md — mark everything complete
# Update CLAUDE.md — remove SOURCE_REPO references, update infra table
# Update README.md — add GitHub App installation instructions
git add docs/ CLAUDE.md README.md
git commit -m "docs: mark production cutover complete"
git push -u origin batch8-cutover
gh pr create --title "docs: production cutover" --body "Marks all remaining work as complete after cutover to teams-for-linux."
```

---

## Dark AI Factory Improvements [DONE]

Completed 2026-03-04. Branch `ci/terraform-in-pipeline`. Implemented Tier 1 and Tier 2 improvements from the dark AI factories survey.

### Task 1: Holdout quality scoring [DONE]

Created `internal/agent/judge.go` with a quality judge that scores research on 5 dimensions (actionability, specificity, trade-offs, completeness, relevance) using a holdout rubric the generating LLM never sees. Scores 0-100. Integrated into `startResearch` in handler.go after safety checks. Added `QualityScore *int` to AuditEntry, migration `005_quality_score.sql`.

### Task 2: Inspiration repos tracking [DONE]

Created `docs/research/inspiration-repos.md` tracking repos and patterns from the dark AI factories survey.

### Task 3: Declarative workflow definitions [DONE]

Created `internal/agent/workflow.go` with Workflow, Transition, and StageConfig types. EnhancementResearchWorkflow captures all 7 stages and 6 transitions as data. Refactored HandleComment to use workflow routing.

### Task 4: Confidence-based approval [DONE]

Added ApprovalModeManual/ApprovalModeConfidence constants, AutoApprovalThreshold = 85, SetApprovalMode(). When in confidence mode, high-quality first-round research auto-approves. Default is manual mode (unchanged behavior).

### Zero-human companies research [DONE]

Created `docs/research/2026-03-04-zero-human-companies.md` surveying the ZHC concept and extracting adoptable patterns. Updated inspiration-repos.md with new entries.

---

## Silent Mode [DONE]

Completed 2026-03-04. Branch `ci/terraform-in-pipeline`. ADR: `docs/decisions/002-silent-mode.md`.

The triage bot was receiving negative reactions on teams-for-linux even for non-AI Phase 1 output. To break the negative perception cycle, the bot now defaults to silent observation mode: all phases still run, results are stored in a `triage_results` table, but no comments are posted to GitHub. The maintainer reviews drafts via the dashboard.

### Changes

- Migration `006_triage_results.sql`: new table with repo, issue_number, issue_title, draft_comment, phases_run, phase_details (JSONB), unique on `(repo, issue_number)`.
- `internal/store/models.go`: added `TriageResultRecord` struct.
- `internal/store/postgres.go`: added `RecordTriageResult` (upsert), `HasTriageResult` (dedup), `GetRecentTriageResults` (dashboard).
- `internal/store/report.go`: added `TotalDrafts` and `RecentDrafts` to `DashboardStats` with queries against `triage_results`.
- `internal/webhook/handler.go`: added `silentMode` field, updated `New()` signature, modified `handleOpened()` to store drafts instead of posting when silent. Added `buildPhaseDetails` for structured JSONB metadata per phase. Dedup check queries both `bot_comments` and `triage_results` in silent mode.
- `cmd/server/main.go`: reads `SILENT_MODE` env var (default `"true"`, only `"false"` enables posting).
- `cmd/dashboard/template.html`: added "Silent Triage Results" section showing issue link, title, phases, phase summary, and date. Hidden when no drafts exist. Added "Silent Drafts" card to stats row.
- `CLAUDE.md`: documented `SILENT_MODE` env var.

Agent sessions in shadow repos are unaffected. The `bot_comments` table is unchanged. Setting `SILENT_MODE=false` restores the original posting behavior.

---

## Future Improvements (from ZHC Research)

Tier 2.5 items identified from the zero-human companies research. Do when the prerequisite conditions are met.

### Task 8: Evaluator-optimizer feedback loop

Feed quality judge feedback back to the research generator for one automatic revision attempt before escalating to humans. If judge scores < 70, re-synthesize with the judge's dimension-level feedback as additional context, capped at one retry.

Prerequisites: Task 1 (quality scoring, done). Trigger: when human reviewers frequently request revisions that the judge could have caught.

Files:
- Modify: `internal/agent/handler.go` — add revision loop in startResearch between judge and comment posting
- Modify: `internal/agent/judge.go` — add per-dimension feedback strings to QualityScore

### Task 9: Reflective supervisor / health monitor

Lightweight scheduled endpoint that queries dashboard stats and quality score distributions, alerting if average quality drops below a threshold or error rate spikes.

Prerequisites: Dashboard (done), quality scoring (done). Trigger: when we want proactive alerting beyond manual dashboard checks.

Files:
- Create: `cmd/health-monitor/main.go` or add `/health-check` endpoint to server
- Modify: `internal/store/report.go` — add quality score aggregation query

### Task 10: Retrospective compliance auditing

Periodic job that reviews past agent outputs using the current safety validators and quality judge, flagging any that would fail today's checks. Catches drift in safety standards.

Prerequisites: Sufficient audit log data. Trigger: after 50+ agent sessions.

Files:
- Create: `cmd/audit/main.go` — retrospective audit tool
- Modify: `internal/store/agent.go` — add query for completed sessions with outputs

### Task 11: Metric-driven threshold adjustment

Track per-repo quality metrics over time (average quality score, human override rate, revision request rate). Recommend threshold adjustments when a category consistently exceeds quality targets.

Prerequisites: Task 4 (confidence-based approval, done), sufficient production data. Trigger: after 100+ scored research documents.

Files:
- Modify: `internal/store/report.go` — add quality metric aggregation queries
- Modify: `cmd/dashboard/main.go` — display quality trends in dashboard
