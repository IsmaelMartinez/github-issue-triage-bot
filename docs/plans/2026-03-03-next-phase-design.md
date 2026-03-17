> **Superseded** — this document has been replaced by `2026-03-03-consolidated-design.md`. Kept for historical reference.

# Next Phase Design: Infrastructure, Data Strategy, and Public Dashboard

Date: 2026-03-03

## Context

The triage bot is deployed on Cloud Run and validated against the test repository. All four phases produce quality output: missing info detection, solution suggestions from troubleshooting docs, duplicate detection from issue history, and misclassification checks. The current deployment (image v9) runs against IsmaelMartinez/triage-bot-test-repo with data from IsmaelMartinez/teams-for-linux.

The database currently holds 18 troubleshooting/configuration documents, 111 issues (from the static issue-index.json), and 0 feature index entries. The teams-for-linux repository actually has 1,356 issues and 781 pull requests, meaning the bot is working with roughly 8% of the available issue data.

This document covers three areas that need to be addressed before cutting over to production: infrastructure hardening (Terraform state, CI/CD), data completeness (seeding strategy), and a public dashboard for transparency.


## 1. Terraform State Backend (GCS)

### Problem

Terraform state currently lives in a local `terraform.tfstate` file. This means only one person can run terraform apply, there's no locking to prevent concurrent modifications, and losing the laptop means losing the state.

### Approach

Create a GCS bucket in the existing GCP project (gen-lang-client-0421325030) with versioning enabled and uniform bucket-level access. Add a `backend "gcs"` block to `terraform/main.tf` and migrate state with `terraform init -migrate-state`.

The bucket name will be `triage-bot-terraform-state` with a `default` prefix. Versioning provides rollback capability if state gets corrupted. No separate lock table is needed since GCS natively supports state locking via object metadata.

### Files to change

`terraform/main.tf` gets the backend block added at the top. A one-time `terraform init -migrate-state` command migrates the existing local state.


## 2. CI/CD via GitHub Actions

### Problem

Deploying currently requires manually building a Docker image, pushing to Artifact Registry, updating terraform.tfvars, and running terraform apply. This led to 9 manual deploy cycles (v1 through v9) during the debugging phase.

### Approach

A single GitHub Actions workflow (`.github/workflows/deploy.yml`) that does three things: runs `go test ./...` on pull requests, and on push to main builds the Docker image, pushes it to Artifact Registry, and deploys to Cloud Run.

The image tag strategy resets from the current v9 to a content-addressed scheme using the short git SHA (e.g., `sha-abc1234`). This eliminates the manual version bumping in terraform.tfvars. The workflow uses Workload Identity Federation (already available in the GCP project) to authenticate to GCP without storing service account keys.

The terraform.tfvars `image_tag` variable becomes unnecessary for CI-driven deploys since the workflow directly calls `gcloud run deploy` with the freshly-built image URI.

### Files to create/change

`.github/workflows/deploy.yml` is the new workflow. `terraform/main.tf` may need a small update to export the Cloud Run service name and region as outputs so the workflow can reference them. The Artifact Registry and Cloud Run service already exist.


## 3. Data Seeding Strategy

This is the section that needs the most deliberation. The bot's quality directly depends on the breadth and depth of data in the database.

### Current state

The database has 111 issues seeded from the static `issue-index.json` that ships with the old bot in teams-for-linux. These 111 issues were hand-picked as representative. The actual repository has 1,356 issues and 781 PRs.

There are 18 documents in the database (14 troubleshooting entries + 4 configuration entries). The old bot also had a feature-index.json with entries covering roadmap items, ADRs, and research docs, but those have not been seeded yet.

### Dimension 1: How many issues to seed

Three options worth considering:

Option A is to seed all 1,356 issues. At 768-dimensional embeddings, this is roughly 4 MB of vector data, well within Neon's free tier. The Gemini embedding API would need approximately 1,400 calls. At the free tier rate limit of 1,500 requests per minute, this completes in under 2 minutes. The advantage is maximum coverage for duplicate detection, and the ivfflat index lists parameter should be bumped from 10 to about 37 (the square root rule suggests sqrt(N) lists). Very old issues (2018-2019 era) about Microsoft Teams classic may produce false positive duplicate matches against modern Teams issues, but the LLM comparison step in Phase 3 should filter those out since the context is clearly different.

Option B is to seed only issues from the last 2-3 years (roughly the last 800-1,000 issues). This reduces noise from the Teams classic era while keeping the dataset large enough for quality duplicate detection. The trade-off is that some older issues with genuinely useful troubleshooting context would be missed.

Option C is the current approach: keep the curated 111 issues. This minimises noise but means the bot can only detect duplicates against a small fraction of the issue history.

Recommendation: Option A (all issues). The 1,356 issue count is small enough that the additional embedding cost is negligible, and the LLM comparison step in Phase 3 already handles false positives from stale issues well. Having more candidates for the vector search to choose from improves recall.

### Dimension 2: Whether to include issue comments

The current seed CLI only stores issue title + body summary (first 200 chars). Comments are where debugging progress, workarounds, and resolution details live. Including them would improve Phase 2 (solution suggestions) since the actual fix is often in a comment, not the issue body.

However, including comments significantly increases the data volume and complexity. A busy issue might have 20-50 comments. Across 1,356 issues, that could be tens of thousands of API calls for fetching and embedding.

Two approaches:

Approach 1 is to embed a concatenated summary of the issue body plus its top 3-5 most-reacted comments, truncated to 2,000 characters total. This captures the most valuable signal (the fix or key discussion) without blowing up the data volume. This would require one additional GitHub API call per issue to fetch comments, but the embedding call is the same (one per issue).

Approach 2 is to keep the current structure (title + body summary only) but make the summary longer, say 500-1,000 characters instead of 200. This gets more of the issue body into the embedding without the complexity of fetching comments.

Recommendation: Start with Approach 2 (longer summaries, 500 chars) for the initial bulk seed. Add comment inclusion as a follow-up enhancement once the bot is in production and we can measure whether it produces better matches.

### Dimension 3: Feature index seeding

The old bot's feature-index.json contains entries covering roadmap items, ADRs, and research documents from the docs-site. These drive Phase 4a (enhancement context), which tells enhancement requesters about existing plans, decisions, or investigations related to their request.

The feature index has not been generated recently, so the entry count from the last run is what we have. The seed CLI already supports `seed features <file>` and the document schema handles it via `doc_type` field (roadmap, adr, research).

The simplest approach is to regenerate the feature index from the current docs-site content using the existing generation script in teams-for-linux, then run the seed CLI. Going forward, this could be automated as part of the CI/CD pipeline: when docs change in teams-for-linux, a workflow calls the seed CLI or an API endpoint on the triage bot to re-embed the changed documents.

### Dimension 4: Seeding implementation

The current seed CLI is straightforward but lacks three things needed for production use: rate limiting (to respect Gemini API quotas), idempotent operation (skip items that already have embeddings), and progress reporting (so you know it's working during a 1,400-item run).

Rather than building all this into the CLI, we can use a pragmatic approach for the initial seed: write a shell script that uses the GitHub API to export all issues as JSON in the format the seed CLI expects, then run the seed CLI with a simple sleep-between-batches wrapper. Rate limiting is simple: process 100 issues, sleep 5 seconds, repeat. Idempotency comes from the UPSERT in the database.

For ongoing maintenance, the webhook handler already updates issue embeddings in real-time when issues are opened, closed, or reopened. So after the initial seed, the data stays current without any additional work.

### Seeding plan (concrete steps)

1. Use GitHub API to export all 1,356 issues as JSON (number, title, state, labels, summary, created_at, closed_at, milestone). Summary should be the first 500 characters of the body, with code fences and HTML stripped.
2. Run the seed CLI against the exported JSON. Add a simple rate limiter (100 items per batch, 5-second pause between batches).
3. Regenerate the feature index from docs-site content and seed it.
4. Update the ivfflat index lists parameter to match the new dataset size.
5. Validate by creating test issues that should match old issues and checking Phase 3 output.


## 4. Public Dashboard

### Purpose

The bot should be a "visible helping agent" that is open and transparent about what it has been doing. A public dashboard shows aggregated data about bot activity, feedback from users (thumbs up/down reactions on bot comments), and the knowledge base the bot draws from.

### What to show

The dashboard should display four categories of information.

Activity summary: total issues triaged, comments posted, breakdown by phase (how often Phase 2 fires vs Phase 3 vs Phase 4a, etc.), and a timeline of recent activity.

Feedback metrics: aggregate thumbs up/down counts on bot comments, broken down by phase. This tells us which phases are producing helpful suggestions and which need improvement. A simple accuracy score (thumbs up / total reactions) gives a single number for overall quality.

Knowledge base stats: number of documents in the database by type (troubleshooting, configuration, roadmap, adr, research), number of issues indexed, and last-updated timestamps. This gives visibility into how fresh the bot's knowledge is.

Recent triage examples: the last 10-20 bot comments with links to the issues, showing what the bot actually said. This lets people judge the quality for themselves.

### Technical approach

Two options for implementation:

Option A is a static dashboard: a GitHub Actions workflow runs on a schedule (daily), queries the database via a small Go CLI or the `/report` endpoint, generates a static HTML page, and publishes it to GitHub Pages. This has zero runtime cost and no additional infrastructure.

Option B is a lightweight dynamic dashboard: add a `/dashboard` endpoint to the existing Cloud Run service that serves an HTML page with data fetched from the database. This shows real-time data but adds a small amount of complexity to the service.

Recommendation: Option A (static dashboard) for the first iteration. It's simpler, has no runtime cost, and fits the "visible helping agent" concept well since the data lives on GitHub Pages alongside the main project docs. The dashboard can be regenerated whenever the bot processes new issues or on a daily schedule.

The static page would be a single HTML file with embedded CSS and minimal JavaScript for rendering charts. No framework needed. The data comes as a JSON blob embedded in the page or fetched from a small JSON file.

### Dashboard location

The dashboard should be accessible from the triage bot repo's GitHub Pages. Since the triage bot repo is private, the dashboard could alternatively be published as part of the teams-for-linux docs-site (which is already public on GitHub Pages). This aligns with the "visible helping agent" concept since users of teams-for-linux are the audience.

### Feedback collection

The bot_comments table already has thumbs_up and thumbs_down columns. What's missing is a mechanism to sync GitHub reaction counts back to the database. Two approaches:

A webhook listener for the `issue_comment` event with `reaction` action would give real-time updates. Alternatively, a scheduled job (GitHub Actions cron) could poll the GitHub API for reactions on all bot comments and update the database. The polling approach is simpler and doesn't require additional webhook configuration.


## 5. Implementation Order

The work breaks down into six phases, roughly in priority order:

Phase 1 is Terraform state migration to GCS. This is a one-time operation with no code changes to the bot itself, just the backend block in main.tf and a state migration command. DONE.

Phase 2 is CI/CD. Set up the GitHub Actions workflow so future changes deploy automatically. DONE.

Phase 3 is data seeding. Export all issues from GitHub API, seed them, seed the feature index. This directly improves bot quality.

Phase 4 is the public dashboard. Build the static page generator, add the report endpoint or CLI, set up GitHub Pages publishing.

Phase 5 is GitHub App conversion. Register a GitHub App, replace PAT-based authentication with JWT/installation tokens using `bradleyfalzon/ghinstallation`, store the private key in Secret Manager. This gives repos one-click installation and eliminates dependency on a personal access token. See ADR 006.

Phase 6 is production cutover to teams-for-linux. Install the GitHub App on teams-for-linux, run both bots in parallel briefly, then remove the old bot workflows and scripts.


## Scope boundaries

This plan intentionally excludes multi-repo support (the bot is designed for it but we're not implementing it yet), authentication/authorization for the dashboard (it's read-only aggregated data), and any changes to the bot's triage logic or comment format.
