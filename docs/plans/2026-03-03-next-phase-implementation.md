> **Superseded** — this document has been replaced by `2026-03-03-consolidated-implementation.md`. Kept for historical reference.

# Next Phase Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Harden infrastructure (GCS state backend, CI/CD), seed all 1,356 issues + feature index into the database, add a public dashboard showing bot activity and feedback, then cut over to production on teams-for-linux.

**Architecture:** Six sequential phases: (1) Terraform state migration to GCS bucket with locking, (2) bulk issue export via GitHub API and seeding via the existing seed CLI with rate limiting, (3) GitHub Actions CI/CD that builds Docker images tagged by git SHA and deploys to Cloud Run, (4) a static dashboard generated from database queries and published to GitHub Pages, (5) convert to GitHub App for proper authentication and one-click repo installation (ADR 006), (6) production cutover.

**Tech Stack:** Go 1.26, Terraform >= 1.5, GCS, GitHub Actions, Gemini embedding API, Neon PostgreSQL + pgvector, Cloud Run v2, GitHub Pages (static HTML).

---

### Task 1: Migrate Terraform state to GCS

**Files:**
- Modify: `terraform/main.tf:7-16` (add backend block inside terraform block)

**Step 1: Create the GCS bucket**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot/terraform
gcloud storage buckets create gs://triage-bot-terraform-state \
  --project=gen-lang-client-0421325030 \
  --location=us-central1 \
  --uniform-bucket-level-access \
  --public-access-prevention
```
Expected: Bucket created successfully.

**Step 2: Enable versioning on the bucket**

Run:
```bash
gcloud storage buckets update gs://triage-bot-terraform-state --versioning
```
Expected: Versioning enabled.

**Step 3: Add the backend block to main.tf**

In `terraform/main.tf`, add a `backend "gcs"` block inside the existing `terraform {}` block, after `required_version`:

```hcl
terraform {
  required_version = ">= 1.5"

  backend "gcs" {
    bucket = "triage-bot-terraform-state"
    prefix = "default"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}
```

**Step 4: Migrate state**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot/terraform
terraform init -migrate-state
```
Expected: Terraform asks to copy existing state to the new backend. Answer "yes". State migrates successfully.

**Step 5: Verify state is in GCS**

Run:
```bash
terraform plan
```
Expected: "No changes. Your infrastructure matches the configuration." This confirms state was migrated correctly and is readable from GCS.

**Step 6: Commit**

```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
git add terraform/main.tf
git commit -m "infra: migrate Terraform state to GCS backend"
```

---

### Task 2: Export all issues from GitHub API

This task creates a Go script that exports all issues from teams-for-linux in the format the seed CLI expects.

**Files:**
- Create: `cmd/export-issues/main.go`

**Step 1: Write the export script**

Create `cmd/export-issues/main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type ghIssue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Body      string     `json:"body"`
	Labels    []ghLabel  `json:"labels"`
	Milestone *ghMilest  `json:"milestone"`
	CreatedAt string     `json:"created_at"`
	ClosedAt  *string    `json:"closed_at"`
	PullReq   *struct{}  `json:"pull_request"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghMilest struct {
	Title string `json:"title"`
}

type seedEntry struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Labels    []string `json:"labels"`
	Summary   string   `json:"summary"`
	CreatedAt string   `json:"created_at"`
	ClosedAt  *string  `json:"closed_at"`
	Milestone *string  `json:"milestone"`
}

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_TOKEN required")
		os.Exit(1)
	}

	repo := "IsmaelMartinez/teams-for-linux"
	if len(os.Args) > 1 {
		repo = os.Args[1]
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var allEntries []seedEntry
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=all&per_page=100&page=%d&direction=asc", repo, page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "request failed page %d: %v\n", page, err)
			os.Exit(1)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "API returned %d: %s\n", resp.StatusCode, string(body))
			os.Exit(1)
		}

		var issues []ghIssue
		if err := json.Unmarshal(body, &issues); err != nil {
			fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
			os.Exit(1)
		}

		if len(issues) == 0 {
			break
		}

		for _, iss := range issues {
			// Skip pull requests (GitHub API returns PRs in /issues)
			if iss.PullReq != nil {
				continue
			}

			labels := make([]string, len(iss.Labels))
			for i, l := range iss.Labels {
				labels[i] = l.Name
			}

			var milestone *string
			if iss.Milestone != nil {
				milestone = &iss.Milestone.Title
			}

			allEntries = append(allEntries, seedEntry{
				Number:    iss.Number,
				Title:     iss.Title,
				State:     iss.State,
				Labels:    labels,
				Summary:   sanitize(iss.Body, 500),
				CreatedAt: iss.CreatedAt,
				ClosedAt:  iss.ClosedAt,
				Milestone: milestone,
			})
		}

		fmt.Fprintf(os.Stderr, "page %d: %d issues (total so far: %d)\n", page, len(issues), len(allEntries))
		page++

		// Respect rate limits
		time.Sleep(500 * time.Millisecond)
	}

	out, _ := json.MarshalIndent(allEntries, "", "  ")
	fmt.Println(string(out))
	fmt.Fprintf(os.Stderr, "exported %d issues\n", len(allEntries))
}

var codeFenceRe = regexp.MustCompile("(?s)```.*?```")
var htmlTagRe = regexp.MustCompile("<[^>]*>")

func sanitize(body string, maxLen int) string {
	result := codeFenceRe.ReplaceAllString(body, "")
	result = htmlTagRe.ReplaceAllString(result, "")
	result = strings.TrimSpace(result)
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}
```

**Step 2: Run the export**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
GITHUB_TOKEN=$(grep github_token terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/export-issues > /tmp/all-issues.json
```
Expected: Prints progress to stderr, outputs JSON to `/tmp/all-issues.json`. Verify count:
```bash
python3 -c "import json; d=json.load(open('/tmp/all-issues.json')); print(len(d))"
```
Expected: ~1,356 issues (PRs filtered out).

**Step 3: Commit**

```bash
git add cmd/export-issues/main.go
git commit -m "feat: add issue export tool for bulk seeding"
```

---

### Task 3: Add rate limiting to seed CLI and seed all issues

**Files:**
- Modify: `cmd/seed/main.go` (add batch size + sleep between batches)

**Step 1: Add rate limiting to seedIssues function**

In `cmd/seed/main.go`, modify the `seedIssues` function to add a sleep every 50 items:

```go
func seedIssues(ctx context.Context, s *store.Store, l *llm.Client, repo string, data []byte, logger *slog.Logger) error {
	var entries []struct {
		Number    int      `json:"number"`
		Title     string   `json:"title"`
		State     string   `json:"state"`
		Labels    []string `json:"labels"`
		Summary   string   `json:"summary"`
		CreatedAt string   `json:"created_at"`
		ClosedAt  *string  `json:"closed_at"`
		Milestone *string  `json:"milestone"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse issue index: %w", err)
	}

	for i, e := range entries {
		text := fmt.Sprintf("%s\n%s", e.Title, e.Summary)
		embedding, err := l.Embed(ctx, text)
		if err != nil {
			return fmt.Errorf("embed issue %d (#%d): %w", i, e.Number, err)
		}

		issue := store.Issue{
			Repo:      repo,
			Number:    e.Number,
			Title:     e.Title,
			Summary:   e.Summary,
			State:     e.State,
			Labels:    e.Labels,
			Milestone: e.Milestone,
			Embedding: embedding,
		}
		if err := s.UpsertIssue(ctx, issue); err != nil {
			return fmt.Errorf("upsert issue %d (#%d): %w", i, e.Number, err)
		}
		logger.Info("seeded issue", "number", e.Number, "index", i, "total", len(entries))

		// Rate limit: pause every 50 items to stay within Gemini API quotas
		if (i+1)%50 == 0 {
			logger.Info("rate limit pause", "processed", i+1, "remaining", len(entries)-i-1)
			time.Sleep(3 * time.Second)
		}
	}
	return nil
}
```

Add the same pattern to `seedFeatures` and `seedTroubleshooting`. Also add `"time"` to the imports.

**Step 2: Run the seed against all exported issues**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
GEMINI_API_KEY=$(grep gemini_api_key terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/seed issues /tmp/all-issues.json
```
Expected: Logs each issue being seeded. Should complete in 2-5 minutes for ~1,356 issues.

**Step 3: Verify issue count in database**

Run:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -c "SELECT count(*) FROM issues WHERE repo = 'IsmaelMartinez/teams-for-linux'"
```
Expected: Count matches the number of exported issues (~1,356).

**Step 4: Commit**

```bash
git add cmd/seed/main.go
git commit -m "feat: add rate limiting to seed CLI"
```

---

### Task 4: Generate and seed the feature index

**Files:**
- No new files needed, uses existing seed CLI

**Step 1: Generate the feature index from docs-site**

The teams-for-linux repo has a workflow that generates the feature index. Run the generation script locally:

```bash
cd /Users/ismael.martinez/projects/github/teams-for-linux
node .github/issue-bot/scripts/generate-feature-index.js > /tmp/feature-index.json
```

If that script doesn't exist standalone or needs workflow context, check for the generation logic in the issue bot workflow and extract it. Alternatively, check if the feature index was previously generated and committed somewhere.

If no generation script exists, create a simple one that scans docs-site/docs/ for ADRs, research docs, and roadmap entries.

**Step 2: Seed the feature index**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
GEMINI_API_KEY=$(grep gemini_api_key terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/seed features /tmp/feature-index.json
```
Expected: Logs each feature being seeded.

**Step 3: Verify document count**

Run:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -c "SELECT doc_type, count(*) FROM documents WHERE repo = 'IsmaelMartinez/teams-for-linux' GROUP BY doc_type"
```
Expected: Shows troubleshooting, configuration, roadmap, adr, research counts.

---

### Task 5: Update ivfflat index for larger dataset

**Files:**
- Create: `migrations/002_update_ivfflat_lists.sql`

**Step 1: Write the migration**

Create `migrations/002_update_ivfflat_lists.sql`:

```sql
-- Update ivfflat index lists to match larger dataset.
-- sqrt(1356) ≈ 37, rounded up to 40 for headroom.
DROP INDEX IF EXISTS idx_issues_embedding;
CREATE INDEX idx_issues_embedding ON issues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 40);

-- Documents table is smaller (~450 entries), sqrt(450) ≈ 21, round to 25.
DROP INDEX IF EXISTS idx_documents_embedding;
CREATE INDEX idx_documents_embedding ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 25);
```

**Step 2: Apply the migration**

Run:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -f migrations/002_update_ivfflat_lists.sql
```
Expected: Indexes recreated without error.

**Step 3: Commit**

```bash
git add migrations/002_update_ivfflat_lists.sql
git commit -m "db: update ivfflat lists for larger dataset"
```

---

### Task 6: Validate seeded data with test issues

**Step 1: Create a test bug issue on the test repo**

Create an issue on IsmaelMartinez/triage-bot-test-repo that should match known old issues, for example a screen sharing issue or Wayland scaling issue.

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "Screen sharing shows black window on Wayland" \
  --body "When I try to share my screen on Wayland, the other participants see a black window. I'm using Fedora 41 with GNOME." \
  --label "bug"
```

**Step 2: Check bot response**

Wait ~10 seconds, then check the bot's comment on the newly created issue. Verify that Phase 3 (duplicate detection) now finds matches from the full issue history, not just the original 111 issues.

**Step 3: Create a test enhancement issue**

```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "Add support for custom notification sounds" \
  --body "It would be great if Teams for Linux could support custom notification sounds instead of the default ones." \
  --label "enhancement"
```

**Step 4: Check Phase 4a response**

Verify the bot's comment includes Phase 4a context from the feature index (roadmap, ADRs, research docs).

---

### Task 7: Create CI/CD GitHub Actions workflow

**Files:**
- Create: `.github/workflows/deploy.yml`

**Step 1: Set up Workload Identity Federation**

Before the workflow can authenticate, we need a Workload Identity Pool and Provider for GitHub Actions. Run:

```bash
# Create workload identity pool
gcloud iam workload-identity-pools create github-actions \
  --project=gen-lang-client-0421325030 \
  --location=global \
  --display-name="GitHub Actions"

# Create provider for the specific repo
gcloud iam workload-identity-pools providers create-oidc github-repo \
  --project=gen-lang-client-0421325030 \
  --location=global \
  --workload-identity-pool=github-actions \
  --display-name="GitHub Repo" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='IsmaelMartinez/github-issue-triage-bot'" \
  --issuer-uri="https://token.actions.githubusercontent.com"

# Create a service account for CI/CD
gcloud iam service-accounts create triage-bot-deploy \
  --project=gen-lang-client-0421325030 \
  --display-name="Triage Bot Deploy"

# Grant it permissions to push to Artifact Registry and deploy to Cloud Run
gcloud projects add-iam-policy-binding gen-lang-client-0421325030 \
  --member="serviceAccount:triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com" \
  --role="roles/artifactregistry.writer"

gcloud projects add-iam-policy-binding gen-lang-client-0421325030 \
  --member="serviceAccount:triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com" \
  --role="roles/run.developer"

gcloud iam service-accounts add-iam-policy-binding \
  triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com \
  --project=gen-lang-client-0421325030 \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/62054333602/locations/global/workloadIdentityPools/github-actions/attribute.repository/IsmaelMartinez/github-issue-triage-bot"
```

**Step 2: Write the workflow file**

Create `.github/workflows/deploy.yml`:

```yaml
name: CI/CD

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read
  id-token: write

env:
  GCP_PROJECT: gen-lang-client-0421325030
  GCP_REGION: us-central1
  AR_REPO: triage-bot
  SERVICE_NAME: triage-bot

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go test ./...

  deploy:
    needs: test
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - id: auth
        uses: google-github-actions/auth@v2
        with:
          workload_identity_provider: projects/62054333602/locations/global/workloadIdentityPools/github-actions/providers/github-repo
          service_account: triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com

      - uses: google-github-actions/setup-gcloud@v2

      - name: Configure Docker for Artifact Registry
        run: gcloud auth configure-docker ${{ env.GCP_REGION }}-docker.pkg.dev --quiet

      - name: Build and push Docker image
        run: |
          IMAGE=${{ env.GCP_REGION }}-docker.pkg.dev/${{ env.GCP_PROJECT }}/${{ env.AR_REPO }}/server:sha-${GITHUB_SHA::7}
          docker build -t $IMAGE .
          docker push $IMAGE

      - name: Deploy to Cloud Run
        run: |
          IMAGE=${{ env.GCP_REGION }}-docker.pkg.dev/${{ env.GCP_PROJECT }}/${{ env.AR_REPO }}/server:sha-${GITHUB_SHA::7}
          gcloud run services update ${{ env.SERVICE_NAME }} \
            --region=${{ env.GCP_REGION }} \
            --image=$IMAGE
```

**Step 3: Commit and push**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci: add GitHub Actions CI/CD workflow"
git push
```

**Step 4: Verify the workflow runs**

Check GitHub Actions tab. The test job should pass. The deploy job should authenticate to GCP, build the image, push it, and update the Cloud Run service.

---

### Task 8: Add report queries to the store

**Files:**
- Modify: `internal/store/postgres.go` (add reporting query methods)
- Create: `internal/store/report.go` (alternative: keep in postgres.go)

**Step 1: Write the reporting query methods**

Create `internal/store/report.go`:

```go
package store

import "context"

// DashboardStats holds aggregated data for the public dashboard.
type DashboardStats struct {
	TotalIssuesTriaged int
	TotalComments      int
	PhaseBreakdown     map[string]int
	TotalThumbsUp      int
	TotalThumbsDown    int
	DocumentCounts     map[string]int
	IssueCount         int
	RecentComments     []RecentComment
}

// RecentComment is a bot comment with metadata for the dashboard.
type RecentComment struct {
	Repo        string
	IssueNumber int
	CommentID   int64
	PhasesRun   []string
	ThumbsUp    int
	ThumbsDown  int
	CreatedAt   string
}

// GetDashboardStats queries all data needed for the public dashboard.
func (s *Store) GetDashboardStats(ctx context.Context, repo string) (*DashboardStats, error) {
	stats := &DashboardStats{
		PhaseBreakdown: make(map[string]int),
		DocumentCounts: make(map[string]int),
	}

	// Total comments and reaction totals
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), coalesce(sum(thumbs_up), 0), coalesce(sum(thumbs_down), 0)
		FROM bot_comments WHERE repo = $1
	`, repo).Scan(&stats.TotalComments, &stats.TotalThumbsUp, &stats.TotalThumbsDown)
	if err != nil {
		return nil, err
	}

	// Phase breakdown (unnest phases_run array and count)
	rows, err := s.pool.Query(ctx, `
		SELECT phase, count(*) FROM bot_comments, unnest(phases_run) AS phase
		WHERE repo = $1 GROUP BY phase
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var phase string
		var count int
		if err := rows.Scan(&phase, &count); err != nil {
			return nil, err
		}
		stats.PhaseBreakdown[phase] = count
	}

	// Document counts by type
	rows2, err := s.pool.Query(ctx, `
		SELECT doc_type, count(*) FROM documents WHERE repo = $1 GROUP BY doc_type
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var docType string
		var count int
		if err := rows2.Scan(&docType, &count); err != nil {
			return nil, err
		}
		stats.DocumentCounts[docType] = count
	}

	// Issue count
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE repo = $1`, repo).Scan(&stats.IssueCount)
	if err != nil {
		return nil, err
	}

	// Recent comments (last 20)
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at::text
		FROM bot_comments WHERE repo = $1
		ORDER BY created_at DESC LIMIT 20
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var rc RecentComment
		if err := rows3.Scan(&rc.Repo, &rc.IssueNumber, &rc.CommentID, &rc.PhasesRun,
			&rc.ThumbsUp, &rc.ThumbsDown, &rc.CreatedAt); err != nil {
			return nil, err
		}
		stats.RecentComments = append(stats.RecentComments, rc)
	}

	return stats, nil
}
```

**Step 2: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: add dashboard stats query methods"
```

---

### Task 9: Add /report endpoint to the server

**Files:**
- Modify: `cmd/server/main.go:63-72` (add /report route)

**Step 1: Add the report handler**

In `cmd/server/main.go`, add a new route after the health check:

```go
mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
    repo := r.URL.Query().Get("repo")
    if repo == "" {
        repo = "IsmaelMartinez/teams-for-linux"
    }
    stats, err := s.GetDashboardStats(r.Context(), repo)
    if err != nil {
        http.Error(w, "failed to get stats", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    json.NewEncoder(w).Encode(stats)
})
```

Add `"encoding/json"` to the imports.

**Step 2: Test locally**

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
GEMINI_API_KEY=$(grep gemini_api_key terraform/terraform.tfvars | cut -d'"' -f2) \
GITHUB_TOKEN=$(grep github_token terraform/terraform.tfvars | cut -d'"' -f2) \
WEBHOOK_SECRET=$(grep webhook_secret terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/server &

curl http://localhost:8080/report | python3 -m json.tool
kill %1
```
Expected: JSON with total comments, phase breakdown, document counts, issue count, recent comments.

**Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: add /report JSON endpoint for dashboard data"
```

---

### Task 10: Create the static dashboard HTML generator

**Files:**
- Create: `cmd/dashboard/main.go`
- Create: `cmd/dashboard/template.html`

**Step 1: Write the dashboard generator**

Create `cmd/dashboard/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL required")
		os.Exit(1)
	}

	repo := os.Getenv("REPO")
	if repo == "" {
		repo = "IsmaelMartinez/teams-for-linux"
	}

	outPath := "docs/dashboard/index.html"
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}

	ctx := context.Background()
	pool, err := store.ConnectPool(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	s := store.New(pool)
	stats, err := s.GetDashboardStats(ctx, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		os.Exit(1)
	}

	statsJSON, _ := json.Marshal(stats)

	tmpl, err := template.ParseFiles("cmd/dashboard/template.html")
	if err != nil {
		fmt.Fprintf(os.Stderr, "template: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create output: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	data := map[string]any{
		"StatsJSON":   template.JS(statsJSON),
		"Repo":        repo,
		"GeneratedAt": time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	}

	if err := tmpl.Execute(f, data); err != nil {
		fmt.Fprintf(os.Stderr, "execute: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "dashboard written to %s\n", outPath)
}
```

**Step 2: Write the HTML template**

Create `cmd/dashboard/template.html` with a clean, single-page HTML dashboard:

The template should be a self-contained HTML file with:
- Embedded CSS (no external dependencies)
- A header showing repo name and generation timestamp
- Cards for: total issues triaged, thumbs up/down ratio, documents indexed, issues indexed
- A table of phase breakdown counts
- A table of recent bot comments with links to the GitHub issues
- Minimal JavaScript to render the stats from the embedded JSON

Keep it simple: no charts library, just well-formatted tables and stat cards with CSS grid. The template receives `{{.StatsJSON}}`, `{{.Repo}}`, and `{{.GeneratedAt}}` via Go template vars.

**Step 3: Test locally**

```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
mkdir -p docs/dashboard
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/dashboard
open docs/dashboard/index.html
```
Expected: A clean HTML page opens showing bot stats.

**Step 4: Commit**

```bash
git add cmd/dashboard/ docs/dashboard/
git commit -m "feat: add static dashboard HTML generator"
```

---

### Task 11: Add reaction sync CLI command

**Files:**
- Create: `cmd/sync-reactions/main.go`

**Step 1: Write the reaction sync tool**

Create `cmd/sync-reactions/main.go` that:
1. Queries bot_comments table for all comment IDs
2. For each, calls GitHub API to get reactions on that comment
3. Updates thumbs_up/thumbs_down columns in the database

This needs a new store method `UpdateReactions(ctx, repo, issueNumber, thumbsUp, thumbsDown)`.

**Step 2: Add UpdateReactions to store**

In `internal/store/postgres.go`, add:

```go
func (s *Store) UpdateReactions(ctx context.Context, repo string, issueNumber int, thumbsUp int, thumbsDown int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE bot_comments SET thumbs_up = $3, thumbs_down = $4
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber, thumbsUp, thumbsDown)
	return err
}

func (s *Store) ListBotComments(ctx context.Context, repo string) ([]BotComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []BotComment
	for rows.Next() {
		var c BotComment
		if err := rows.Scan(&c.ID, &c.Repo, &c.IssueNumber, &c.CommentID,
			&c.PhasesRun, &c.ThumbsUp, &c.ThumbsDown, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
```

**Step 3: Commit**

```bash
git add cmd/sync-reactions/main.go internal/store/postgres.go
git commit -m "feat: add reaction sync tool and store methods"
```

---

### Task 12: Add GitHub Actions workflow for dashboard generation

**Files:**
- Create: `.github/workflows/dashboard.yml`

**Step 1: Write the dashboard workflow**

Create `.github/workflows/dashboard.yml`:

```yaml
name: Update Dashboard

on:
  schedule:
    - cron: '0 6 * * *'  # Daily at 6am UTC
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
```

**Step 2: Add repository secrets**

In the github-issue-triage-bot repo settings, add secrets:
- `DATABASE_URL` — the Neon connection string
- `BOT_GITHUB_TOKEN` — the GitHub token for API calls

**Step 3: Commit**

```bash
git add .github/workflows/dashboard.yml
git commit -m "ci: add daily dashboard generation workflow"
```

---

### Task 13: Clean up export tool after seeding

**Step 1: Remove the export-issues tool**

After successful seeding (Task 3), the export tool is no longer needed as ongoing issue updates happen via webhooks.

```bash
rm -rf cmd/export-issues
git add -A cmd/export-issues
git commit -m "chore: remove export-issues tool (seeding complete)"
```

---

### Task 14: Production cutover to teams-for-linux

**Step 1: Configure webhook on teams-for-linux**

```bash
gh api repos/IsmaelMartinez/teams-for-linux/hooks \
  --method POST \
  -f url="https://triage-bot-lhuutxzbnq-uc.a.run.app/webhook" \
  -f content_type="json" \
  -f secret="$(grep webhook_secret /Users/ismael.martinez/projects/github/github-issue-triage-bot/terraform/terraform.tfvars | cut -d'"' -f2)" \
  -f "events[]=issues"
```

**Step 2: Clear SOURCE_REPO override**

Update terraform.tfvars to set `source_repo = ""` (empty) since the webhook repo and data repo will be the same.

Run:
```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot/terraform
terraform apply
```

**Step 3: Create a test issue on teams-for-linux**

Create a test issue on teams-for-linux, verify the bot responds correctly, then close and delete the test issue.

**Step 4: Disable old bot workflows**

In teams-for-linux, disable the old triage bot GitHub Actions workflows. Don't delete them yet — keep them around for a week as a fallback.

**Step 5: Update remaining-work.md**

Update `docs/decisions/001-remaining-work.md` to mark cutover as complete.

---

### Task 15: Convert to GitHub App

See ADR 006 for the full rationale. This replaces the current PAT-based webhook with a registered GitHub App.

**Files:**
- Modify: `internal/github/client.go` (replace PAT auth with installation token auth)
- Modify: `cmd/server/main.go` (load app private key, pass app ID)
- Modify: `go.mod` (add `bradleyfalzon/ghinstallation/v2`)
- Modify: `terraform/main.tf` (add Secret Manager resource for private key)

**Step 1: Register the GitHub App**

Go to https://github.com/settings/apps/new and configure:
- App name: `teams-for-linux-triage-bot` (or similar)
- Webhook URL: `https://triage-bot-lhuutxzbnq-uc.a.run.app/webhook`
- Webhook secret: same as current WEBHOOK_SECRET
- Permissions: Issues (read/write)
- Subscribe to events: Issues
- Where can this app be installed: Only on this account

After creation, note the App ID and generate a private key (.pem file).

**Step 2: Add ghinstallation dependency**

```bash
cd /Users/ismael.martinez/projects/github/github-issue-triage-bot
go get github.com/bradleyfalzon/ghinstallation/v2
```

**Step 3: Update the GitHub client**

Replace the PAT-based client in `internal/github/client.go` with one that accepts either a PAT or app credentials. The `ghinstallation` library provides an `http.Transport` that handles JWT signing and installation token refresh automatically. The installation ID comes from the webhook payload's `installation.id` field.

**Step 4: Update the webhook handler**

In `internal/webhook/handler.go`, extract `installation.id` from the webhook JSON payload and pass it to the GitHub client so it can obtain the correct installation token.

**Step 5: Store the private key in Secret Manager**

```bash
gcloud secrets create triage-bot-app-key --project=gen-lang-client-0421325030
gcloud secrets versions add triage-bot-app-key --data-file=path/to/private-key.pem
```

Update `terraform/main.tf` to mount the secret as a volume or environment variable in the Cloud Run service.

**Step 6: Test on triage-bot-test-repo**

Install the app on `IsmaelMartinez/triage-bot-test-repo`. Create test issues to verify the bot posts comments using the app identity instead of the PAT user identity.

**Step 7: Install on teams-for-linux**

Once validated, install the app on `IsmaelMartinez/teams-for-linux`. Remove the old webhook configuration and PAT-based setup. Remove the GITHUB_TOKEN environment variable from Cloud Run since the app authenticates with its own key.

**Step 8: Commit**

```bash
git add internal/github/ cmd/server/main.go go.mod go.sum terraform/main.tf
git commit -m "feat: convert to GitHub App authentication (ADR 006)"
```
