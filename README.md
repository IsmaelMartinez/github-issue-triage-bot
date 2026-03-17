# GitHub Issue Triage Bot

Helps maintainers of [Teams for Linux](https://github.com/IsmaelMartinez/teams-for-linux) triage GitHub issues faster by checking bug reports for completeness and surfacing relevant project documentation. When a new issue is opened, the bot analyzes its content and provides useful context drawn from troubleshooting guides, upstream Electron release notes, architecture decisions, and past issues.

## What the bot does

### Missing information check

When a bug report is missing key details — reproduction steps, debug logs, expected behavior, or web app reproducibility — the bot flags exactly what's missing and shows how to provide it. This is pure template parsing with no AI involved; it compares the issue body against the project's bug report form and identifies unfilled sections.

### Known solutions

The bot searches project documentation for content that might help resolve the bug. This includes troubleshooting guides, configuration docs, upstream Electron release notes, and similar past issues. Matches above a relevance threshold are suggested with direct links. This phase uses Gemini AI for semantic matching against embedded documents stored in a vector database.

### Related context for enhancements

For feature requests, the bot searches architecture decision records, roadmap items, and research documents to surface existing context about similar ideas. A maintainer reviews the context brief in a private shadow repository before any output reaches the public issue. The maintainer can request deeper research synthesis, acknowledge the context, or discard it.

All bot output passes through two quality layers — a structural validator (length limits, URL allowlists, mention blocking) and an LLM reviewer (relevance, tone, prompt injection detection). The bot escalates to a human if it cannot produce useful output within 4 attempts.

### What the bot doesn't do

The bot doesn't detect duplicate issues — GitHub's native search and issue templates handle that adequately. It doesn't auto-label, auto-close, or auto-assign issues. Suggestions are grounded in project-specific documentation, not general knowledge, so it won't answer questions outside the scope of teams-for-linux.

## Feedback

React with a thumbs up or thumbs down on bot comments, or mention @ismael-triage-bot with specific feedback in a reply. When you edit your bug report to fill in sections the bot flagged as missing, that's tracked automatically as a positive signal.

## Dashboard

A live dashboard showing triage activity, phase hit rates, and agent session status is available at https://triage-bot-lhuutxzbnq-uc.a.run.app/dashboard. A daily snapshot is also published to [GitHub Pages](https://ismaelmartinez.github.io/github-issue-triage-bot/).

---

## Development

### Prerequisites

Go 1.26+, Docker, Terraform >= 1.5, and a GCP project with Cloud Run and Artifact Registry enabled.

### Local setup

```bash
# Run tests
go test ./...

# Start local PostgreSQL with pgvector
docker-compose up -d

# Run the server
DATABASE_URL="..." GEMINI_API_KEY="..." GITHUB_APP_ID="..." GITHUB_PRIVATE_KEY="..." WEBHOOK_SECRET="..." go run ./cmd/server
```

### Architecture

```
GitHub Webhook (issue + comment + edit events)
        |
        v
Cloud Run (Go binary)
        |
        +-- Triage Pipeline
        |       +-- Missing info check: template parsing (no LLM)
        |       +-- Known solutions: pgvector search + Gemini (bugs)
        |       +-- Related context: pgvector search + Gemini (enhancements)
        |       |
        |       v
        |   Post comment / store for dashboard (silent mode)
        |
        +-- Enhancement Researcher Agent (if shadow repo configured)
        |       +-- Create mirror issue in shadow repo
        |       +-- Post context brief (pgvector + Gemini summary)
        |       +-- Maintainer signals: research / use as context / reject
        |       +-- Full research pipeline (if research signal)
        |       +-- Safety layers (structural + LLM)
        |       +-- Publish summary to public issue
        |
        +-- Feedback Tracking
        |       +-- Issue edit detection (Phase 1 fill rate)
        |       +-- @mention capture (qualitative feedback)
        |
        +-- Health Monitor (/health-check)
                +-- Confidence score trends
                +-- Stuck session detection
                +-- Orphaned triage detection
        |
        v
Neon PostgreSQL + pgvector             GitHub Pages Dashboard
(documents, issues, bot_comments,      (daily via cmd/dashboard)
 agent_sessions, feedback_signals)
```

### Silent mode

The bot defaults to silent (observer) mode, controlled by the `SILENT_MODE` environment variable (default: `"true"`). In silent mode, triage results are stored in the database for dashboard review but no comments are posted on GitHub issues. Set `SILENT_MODE=false` to enable public commenting. Agent sessions in shadow repos are unaffected by this setting. See `docs/decisions/002-silent-mode.md` for the full rationale.

### Environment variables

The server requires `DATABASE_URL`, `GITHUB_APP_ID`, `GITHUB_PRIVATE_KEY`, and `WEBHOOK_SECRET`. `GEMINI_API_KEY` is optional (the bot logs a warning and skips LLM phases if unset). `GITHUB_PRIVATE_KEY` should contain the PEM content either as raw PEM text or base64-encoded PEM.

Optional variables controlling runtime behavior: `SOURCE_REPO` overrides the repository used for vector similarity searches (useful when testing against a different repo than the one sending webhooks). `SILENT_MODE` (default `"true"`) controls whether triage comments are posted publicly or stored silently for dashboard review — set to `"false"` to enable posting. `SHADOW_REPOS` defines shadow repo mappings for the Enhancement Researcher agent as comma-separated `owner/repo:owner/shadow` pairs (e.g., `IsmaelMartinez/teams-for-linux:IsmaelMartinez/triage-bot-shadow`). `PORT` sets the HTTP listen port (defaults to 8080).

### Deployment

Pushes to `main` automatically build and deploy via GitHub Actions. The workflow builds a Docker image tagged with the git SHA, pushes to Artifact Registry, and updates the Cloud Run service. Authentication uses Workload Identity Federation (no service account keys).

Manual deployment:
```bash
cd terraform
terraform plan
terraform apply
```

### Seeding

The database needs an initial seed of documentation and issue data:

```bash
# Seed troubleshooting docs
go run ./cmd/seed troubleshooting path/to/troubleshooting-index.json

# Seed issues
go run ./cmd/seed issues path/to/issue-index.json

# Seed feature index (roadmap, ADRs, research)
go run ./cmd/seed features path/to/feature-index.json

# Seed upstream dependency docs (e.g. Electron release notes)
go run ./cmd/seed upstream path/to/electron-v39-releases.json
```

After the initial seed, the webhook handler keeps issue data up to date in real-time. A `workflow_dispatch` seed workflow also enables re-seeding from the GitHub Actions UI.

## Infrastructure

Terraform manages the GCP resources (Cloud Run, Artifact Registry, billing budget). State is stored in a GCS bucket with versioning and locking. The database is Neon PostgreSQL with pgvector, managed outside Terraform.

See `docs/decisions/` for architecture decision records and `docs/plans/` for implementation plans.

## Installing the GitHub App

To use this bot on your own repository:

1. Register a GitHub App at https://github.com/settings/apps/new with permissions: Issues (read & write), Contents (read & write), Pull requests (read & write). Subscribe to "Issues", "Issue comments", and "Issue edits" webhook events. Set the webhook URL to your Cloud Run service URL + `/webhook`.
2. Generate and download a private key PEM file from the App settings.
3. Install the App on the target repository. If using the Enhancement Researcher agent, also install it on the shadow repository so the bot can create mirror issues and respond to approval signals there.
4. Set the environment variables: `GITHUB_APP_ID` (numeric App ID from settings), `GITHUB_PRIVATE_KEY` (base64-encoded PEM or raw PEM content), and `WEBHOOK_SECRET` (the secret you configured when creating the App).
5. Deploy via `terraform apply` or set the secrets in your CI/CD environment.
