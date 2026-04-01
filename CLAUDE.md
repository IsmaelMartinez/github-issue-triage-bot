# CLAUDE.md

## Project Overview

Repository Strategist: a GitHub App that maintains institutional memory for software projects and uses it to provide doc-grounded triage and strategic intelligence. Currently deployed against IsmaelMartinez/teams-for-linux. Searches project-specific documentation (troubleshooting guides, ADRs, roadmap, research docs, upstream dependency releases) via vector similarity to help with bug reports and enhancement requests. Evolving to add proactive pattern detection, roadmap suggestions, and project health briefings (see `docs/plans/2026-03-18-repository-strategist-design.md`). Deployed as a serverless container on Google Cloud Run, backed by Neon PostgreSQL with pgvector, and Gemini 2.5 Flash for LLM generation and embeddings.

## Essential Commands

```bash
# Run tests
go test ./...

# Run vet
go vet ./...

# Run linter (matches CI)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
golangci-lint run ./...

# Build server, seed, and sync-reactions binaries
go build -o server ./cmd/server
go build -o seed ./cmd/seed
go build -o sync-reactions ./cmd/sync-reactions

# Sync GitHub reactions to bot comments in DB (requires DATABASE_URL, GITHUB_TOKEN)
go run ./cmd/sync-reactions

# Run locally (requires DATABASE_URL, GEMINI_API_KEY, GITHUB_APP_ID, GITHUB_PRIVATE_KEY, WEBHOOK_SECRET)
go run ./cmd/server

# Run MCP server (requires running triage bot at TRIAGE_BOT_URL)
TRIAGE_BOT_URL=https://triage-bot-lhuutxzbnq-uc.a.run.app go run ./cmd/mcp

# Add to Claude Code
claude mcp add triage-bot -- go run ./cmd/mcp

# Docker build (linux/amd64 for Cloud Run)
docker build --platform linux/amd64 -t us-central1-docker.pkg.dev/gen-lang-client-0421325030/triage-bot/server:latest .

# Local dev with docker-compose (PostgreSQL + pgvector)
docker-compose up -d

# Terraform (state in GCS, needs gcloud auth or GOOGLE_OAUTH_ACCESS_TOKEN)
cd terraform && terraform plan && terraform apply

# Seed database
./seed troubleshooting <path-to-troubleshooting-index.json>
./seed issues <path-to-issue-index.json>
./seed features <path-to-feature-index.json>

# Generate feature seed index from teams-for-linux docs (ADRs, research, roadmap)
./scripts/generate-feature-index.sh > data/feature-index.json

# Seed via GitHub Actions (workflow_dispatch)
gh workflow run seed.yml -f seed_type=features -f data_file=data/feature-index.json

# Seed upstream dependency docs (e.g. Electron release notes)
./scripts/generate-upstream-index.sh --repo electron/electron --type releases --version 39 --doc-type upstream_release > data/electron-v39-releases.json
./seed upstream data/electron-v39-releases.json
```

## Architecture

The service receives GitHub webhook events (issue opened/closed/reopened, issue comments, issue edits) and runs a focused triage pipeline. Phase 3 (duplicate detection) and Phase 4b (misclassification) were removed in favour of GitHub native tooling — see `docs/plans/2026-03-15-lean-bot-pivot-design.md`.

Phase 1 (pure parsing, no LLM): detects missing information in bug reports by checking form sections against known templates. Phase 2 (pgvector + LLM): embeds the issue, searches all document types (troubleshooting, configuration, ADR, roadmap, research) for similar entries with per-category relevance thresholds (troubleshooting 70%, ADR/roadmap/research 55%, configuration 50%), sends top-5 to Gemini to generate suggestions. Phase 4a (pgvector + LLM): for enhancement requests, searches roadmap/ADR/research documents for related context.

All phase results are consolidated into a single markdown comment by the comment builder (concise format: no greeting, compact footer with feedback hint). When a shadow repo is configured, the triage comment is posted there for maintainer review; on `lgtm`, a curated summary is promoted to the original public issue. The bot also tracks feedback signals: when users edit their issue to fill in Phase 1 flagged sections (via `issues.edited` webhook), and when users @mention the bot. A `/health-check` endpoint monitors confidence score trends, stuck sessions, and orphaned triage, creating GitHub alert issues when thresholds are violated.

The vector store includes upstream dependency docs (Electron release notes, changelogs) in addition to project-specific docs, so Phase 2 can surface relevant upstream changes when triaging bug reports.

The event journal (`repo_events` table) records all webhook events (issues, comments, pushes) and daily-scraped events (merged PRs, releases) for temporal analysis. On push to the default branch, auto-ingest detects changed documentation files matching the per-repo `butler.json` config's `doc_paths` patterns, fetches their content via the GitHub Contents API, embeds them, and upserts into the vector store. A cross-reference index (`doc_references` table) tracks `#NNN` issue and `ADR-NNN` document references extracted from content via regex.

The synthesis engine (`internal/synthesis/`) runs three analysers weekly: cluster detection (groups recent issues by embedding cosine similarity via union-find), decision drift detection (flags ADRs contradicted by merged PRs and stale roadmap items), and upstream impact analysis (cross-references new upstream releases against existing ADRs/roadmap). Findings are combined into a `[Briefing]` shadow issue posted to the shadow repo. The `/synthesize` POST endpoint triggers synthesis on demand; a Monday cron workflow triggers it weekly.

For enhancement issues with a configured shadow repo, the bot also starts an agent session. The agent creates a mirror issue and posts a context brief with relevant ADRs, roadmap items, and similar past issues from vector search, plus a short LLM-generated summary. The maintainer can reply `research` to trigger full Gemini research synthesis, `use as context` to acknowledge and close the session, or `reject` to discard. All agent outputs pass through two safety layers: a structural validator (length, URL hosts, mentions, control characters) and an LLM reviewer (relevance, tone, prompt injection detection). The agent escalates to a human after 4 round-trips without reaching review.

## Project Structure

```
cmd/server/main.go           # HTTP server entry point (/webhook, /health, /health-check, /ingest, /synthesize, /report, /report/trends, /dashboard)
cmd/server/dashboard.go       # Live dashboard handler (go:embed template, /dashboard endpoint)
cmd/server/template.html      # Dashboard template (sidebar layout, Chart.js charts, drill-down)
cmd/seed/main.go              # CLI to import JSON indexes into database
cmd/sync-reactions/main.go   # Sync GitHub reactions to bot comments in DB
cmd/backfill/main.go          # One-time backfill of triage results for historical issues
cmd/mcp/main.go               # MCP server (stdio JSON-RPC, wraps HTTP endpoints for Claude Code agents)
internal/webhook/handler.go   # Webhook verification, replay protection, routing
internal/phases/              # Triage phases (phase1.go, phase2.go, phase4a.go)
internal/comment/builder.go   # Consolidates phase results into markdown
internal/comment/sanitize.go  # LLM output and URL sanitization
internal/llm/client.go        # Gemini API client (generation + embeddings)
internal/github/client.go     # GitHub App client (comments, issues, branches, PRs)
internal/mirror/              # Shadow repo mirroring (push events)
internal/store/postgres.go    # PostgreSQL + pgvector queries
internal/store/agent.go       # Agent session, audit log, and approval gate queries
internal/store/report.go      # Dashboard stats queries
internal/store/health.go      # Health monitor queries and threshold evaluation
internal/store/feedback.go    # Feedback signal storage and stats
internal/store/models.go      # Shared data types (includes agent stage/gate constants)
internal/store/events.go      # Event journal (repo_events) queries
internal/store/references.go  # Cross-reference index (doc_references) queries
internal/agent/handler.go     # Agent handler: context brief (default) and research flows
internal/agent/orchestrator.go # Approval signal parsing (lgtm, revise, reject, publish)
internal/agent/research.go    # Enhancement analysis and research synthesis prompts
internal/synthesis/types.go   # Synthesizer interface and Finding type
internal/synthesis/runner.go  # Orchestrates synthesizers, posts briefing to shadow repo
internal/synthesis/clusters.go # Issue cluster detection (cosine similarity, union-find)
internal/synthesis/drift.go   # Decision drift and roadmap staleness detection
internal/synthesis/upstream.go # Upstream impact analysis (release-vs-ADR cross-reference)
internal/synthesis/briefing.go # Markdown briefing generator
internal/config/butler.go     # Per-repo butler.json config parser
internal/config/loader.go     # Config cache with TTL and GitHub Contents API reader
internal/ingest/embed.go      # Shared document embedding and upsert logic
internal/safety/structural.go # Deterministic safety validator (length, URLs, mentions)
internal/safety/llm_validator.go # LLM-based safety reviewer (relevance, tone, injection)
internal/runner/runner.go     # Runner interface for task execution abstraction
internal/runner/inprocess.go  # In-process runner (goroutines with context timeout)
internal/mcp/protocol.go      # MCP JSON-RPC 2.0 protocol implementation
internal/mcp/tools/            # MCP tool implementations (health, trends, triage, briefing)
scripts/generate-feature-index.sh # Generate seed JSON from teams-for-linux ADRs/research/roadmap
scripts/generate-upstream-index.sh # Generate seed JSON from upstream dependency releases/changelogs
data/                         # Seed data (feature index, Electron upstream docs)
internal/webhook/journal.go   # Event journal writes from webhook events
internal/webhook/autoingest.go # Auto-ingest changed docs on push
migrations/                   # Database migrations (001-012)
terraform/main.tf             # GCP infrastructure (Cloud Run, AR, budget, secrets)
.github/workflows/deploy.yml  # CI/CD: test on PR, build+deploy on push to main
.github/workflows/dashboard.yml # Daily maintenance: stale cleanup, health check, reaction sync
.github/workflows/seed.yml    # Manual seed workflow (workflow_dispatch)
.github/workflows/event-ingest.yml # Daily event ingestion from GitHub API
.github/workflows/synthesis.yml    # Weekly synthesis briefing cron (Monday 06:00 UTC)
docs/decisions/               # Architecture decision records
docs/plans/                   # Implementation plans and design docs
.golangci.yml                 # Linter configuration
CONTRIBUTING.md               # Developer setup and contribution guidelines
```

## Infrastructure

| Resource | Value |
|---|---|
| GCP project | gen-lang-client-0421325030 |
| Cloud Run URL | https://triage-bot-lhuutxzbnq-uc.a.run.app |
| Live Dashboard | https://triage-bot-lhuutxzbnq-uc.a.run.app/dashboard |
| Artifact Registry | us-central1-docker.pkg.dev/gen-lang-client-0421325030/triage-bot |
| Neon project | falling-resonance-06310725 (aws-us-east-2) |
| Database | PostgreSQL 17 + pgvector 0.8.0 |
| LLM | Gemini 2.5 Flash (generation) + gemini-embedding-001 (768-dim) |
| Budget | GBP 15/month with alerts at 5%, 25%, 50% |
| Terraform state | gs://triage-bot-terraform-state (GCS, versioned, locked) |
| CI/CD | GitHub Actions (.github/workflows/deploy.yml) |
| WIF pool | projects/62054333602/locations/global/workloadIdentityPools/github-actions |
| Deploy SA | triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com |

Secrets are in terraform/terraform.tfvars (gitignored, never committed). CI/CD secrets are in GitHub repo settings.

## Development Patterns

Go 1.26 with standard library where possible. External dependencies: pgx/v5 (PostgreSQL driver), pgvector-go, and ghinstallation/v2 (GitHub App authentication). Tests use table-driven patterns and Go's built-in testing. No mocking frameworks.

The Gemini API client uses the REST API directly rather than an SDK to minimize dependencies. JSON responses are parsed with `responseMimeType: application/json` for structured output.

Phase 1 is pure string parsing (no network calls) and has the most comprehensive test coverage. The LLM phases are harder to unit test since they depend on Gemini's output format; they use extractJSONArray/ExtractJSONObject helpers with fallback parsing. All LLM JSON responses must be passed through `phases.ExtractJSONObject()` before `json.Unmarshal`, even when using `responseMimeType: application/json`, as a defensive measure against code-fenced or prefixed responses.

The comment builder produces concise output: a single-sentence preamble, no greeting line, a compact footer with a feedback hint. Keep builder output minimal.

Environment variables: DATABASE_URL (required), GEMINI_API_KEY (optional, warns if missing), GITHUB_APP_ID (required, numeric App ID), GITHUB_PRIVATE_KEY (required, base64-encoded or raw PEM), WEBHOOK_SECRET (required), SOURCE_REPO (optional, overrides repo for vector searches), SHADOW_REPOS (optional, comma-separated "owner/repo:owner/shadow" mappings for agent sessions), INGEST_SECRET (optional, authenticates /ingest and /synthesize endpoints; empty disables auth), PORT (optional, defaults to 8080). The cmd/sync-reactions tool uses REPO (optional, defaults to IsmaelMartinez/teams-for-linux) to select which repository's comments to sync.

## Issue Template Headers

Phase 1 parses section headers from the teams-for-linux bug report form template. The primary headers are: `Reproduction steps`, `Expected Behavior`, `Debug`, `Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?`. Both `##` and `###` heading levels are accepted. Synonym headers are also recognised for reproduction steps (`Steps to reproduce`, `How to reproduce`) and expected behavior (`Expected Behaviour` with British spelling). The heading guard uses a start-of-line regex to avoid matching `## ` inside code blocks or prose.

## Key Decisions

See docs/decisions/ for full records. Summary: chose Gemini for its free tier and embedding API, Neon for managed pgvector with connection pooling, Cloud Run for serverless Go with fast cold starts.

## Linting

The project uses golangci-lint v1.64.8 with errcheck, govet, staticcheck, gocritic, and other linters configured in `.golangci.yml`. CI runs linting on every PR. See CONTRIBUTING.md for full development setup and code style guidelines.

## Repo Butler

This repo is monitored by [Repo Butler](https://github.com/IsmaelMartinez/repo-butler), a portfolio health agent that observes repo health daily and generates dashboards, governance proposals, and tier classifications.

**Your report:** https://ismaelmartinez.github.io/repo-butler/github-issue-triage-bot.html
**Portfolio dashboard:** https://ismaelmartinez.github.io/repo-butler/
**Consumer guide:** https://github.com/IsmaelMartinez/repo-butler/blob/main/docs/consumer-guide.md

### Querying Reginald (the butler MCP server)

To query your repo's health tier, governance findings, and portfolio data from any Claude Code session, add the MCP server once (adjust the path to your local repo-butler checkout):

```bash
claude mcp add repo-butler node /path/to/repo-butler/src/mcp.js
```

Available tools: `get_health_tier`, `get_campaign_status`, `query_portfolio`, `get_snapshot_diff`, `get_governance_findings`.

When working on health improvements, check the per-repo report for the current tier checklist and use the consumer guide for fix instructions.
