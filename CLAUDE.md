# CLAUDE.md

## Project Overview

GitHub Issue Triage Bot: a standalone Go service that automatically triages issues on IsmaelMartinez/teams-for-linux by analyzing issue content and posting helpful comments. Deployed as a serverless container on Google Cloud Run, backed by Neon PostgreSQL with pgvector for vector similarity search, and Gemini 2.5 Flash for LLM generation and embeddings.

## Essential Commands

```bash
# Run tests
go test ./...

# Run vet
go vet ./...

# Run linter (matches CI)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
golangci-lint run ./...

# Build server, seed, dashboard, and sync-reactions binaries
go build -o server ./cmd/server
go build -o seed ./cmd/seed
go build -o dashboard ./cmd/dashboard
go build -o sync-reactions ./cmd/sync-reactions

# Generate dashboard HTML (requires DATABASE_URL)
go run ./cmd/dashboard [output-path]

# Sync GitHub reactions to bot comments in DB (requires DATABASE_URL, GITHUB_TOKEN)
go run ./cmd/sync-reactions

# Run locally (requires DATABASE_URL, GEMINI_API_KEY, GITHUB_APP_ID, GITHUB_PRIVATE_KEY, WEBHOOK_SECRET)
go run ./cmd/server

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
```

## Architecture

The service receives GitHub webhook events (issue opened/closed/reopened, issue comments) and runs a multi-phase triage pipeline:

Phase 1 (pure parsing, no LLM): detects missing information in bug reports by checking form sections against known templates. Phase 2 (pgvector + LLM): embeds the issue, searches the troubleshooting/configuration document store for similar entries, sends top-5 to Gemini to generate solution suggestions. Phase 3 (pgvector + LLM): embeds the issue, searches the issue store for similar past issues, sends top-5 to Gemini to identify potential duplicates. Phase 4a (pgvector + LLM): for enhancement requests, searches roadmap/ADR/research documents for related context. Phase 4b (LLM only): checks if the issue might be misclassified (bug vs enhancement vs question).

All phase results are consolidated into a single markdown comment by the comment builder. When a shadow repo is configured, the triage comment is posted there for maintainer review; on `lgtm`, a curated summary is promoted to the original public issue.

For enhancement issues with a configured shadow repo, the bot also starts an agent session. The Enhancement Researcher agent progresses through a state machine (NEW, CLARIFYING, RESEARCHING, REVIEW_PENDING, REVISION, APPROVED, COMPLETE) in a private shadow repository. It analyzes the enhancement, optionally asks clarifying questions, synthesizes a research document using pgvector context, and waits for maintainer approval. On approval, it commits the research document and opens a PR. On "publish"/"promote", it posts a curated summary on the original public issue. All agent outputs pass through two safety layers: a structural validator (length, URL hosts, mentions, control characters) and an LLM reviewer (relevance, tone, prompt injection detection). The agent escalates to a human after 4 round-trips without reaching review.

## Project Structure

```
cmd/server/main.go           # HTTP server entry point (/webhook, /health, /report)
cmd/seed/main.go              # CLI to import JSON indexes into database
cmd/dashboard/main.go         # Static dashboard HTML generator
cmd/sync-reactions/main.go   # Sync GitHub reactions to bot comments in DB
internal/webhook/handler.go   # Webhook verification, replay protection, routing
internal/phases/              # Triage phases (phase1.go through phase4b.go)
internal/comment/builder.go   # Consolidates phase results into markdown
internal/comment/sanitize.go  # LLM output and URL sanitization
internal/llm/client.go        # Gemini API client (generation + embeddings)
internal/github/client.go     # GitHub App client (comments, issues, branches, PRs)
internal/store/postgres.go    # PostgreSQL + pgvector queries
internal/store/agent.go       # Agent session, audit log, and approval gate queries
internal/store/report.go      # Dashboard stats queries
internal/store/models.go      # Shared data types (includes agent stage/gate constants)
internal/agent/handler.go     # Agent state machine and webhook comment handler
internal/agent/orchestrator.go # Approval signal parsing (lgtm, revise, reject, publish)
internal/agent/research.go    # Enhancement analysis and research synthesis prompts
internal/safety/structural.go # Deterministic safety validator (length, URLs, mentions)
internal/safety/llm_validator.go # LLM-based safety reviewer (relevance, tone, injection)
internal/runner/runner.go     # Runner interface for task execution abstraction
internal/runner/inprocess.go  # In-process runner (goroutines with context timeout)
migrations/                   # Database migrations (001-004)
terraform/main.tf             # GCP infrastructure (Cloud Run, AR, budget, secrets)
.github/workflows/deploy.yml  # CI/CD: test on PR, build+deploy on push to main
.github/workflows/dashboard.yml # Daily dashboard generation + GitHub Pages
docs/decisions/               # Architecture decision records
docs/plans/                   # Implementation plans
.golangci.yml                 # Linter configuration
CONTRIBUTING.md               # Developer setup and contribution guidelines
```

## Infrastructure

| Resource | Value |
|---|---|
| GCP project | gen-lang-client-0421325030 |
| Cloud Run URL | https://triage-bot-lhuutxzbnq-uc.a.run.app |
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

Phase 1 is pure string parsing (no network calls) and has the most comprehensive test coverage. The LLM phases are harder to unit test since they depend on Gemini's output format; they use extractJSONArray/extractJSONObject helpers with fallback parsing.

Environment variables: DATABASE_URL (required), GEMINI_API_KEY (optional, warns if missing), GITHUB_APP_ID (required, numeric App ID), GITHUB_PRIVATE_KEY (required, base64-encoded or raw PEM), WEBHOOK_SECRET (required), SOURCE_REPO (optional, overrides repo for vector searches), SHADOW_REPOS (optional, comma-separated "owner/repo:owner/shadow" mappings for agent sessions), PORT (optional, defaults to 8080). The cmd/sync-reactions tool uses REPO (optional, defaults to IsmaelMartinez/teams-for-linux) to select which repository's comments to sync. The cmd/dashboard tool uses DASHBOARD_REPO (optional, defaults to IsmaelMartinez/teams-for-linux).

## Issue Template Headers

Phase 1 parses these exact section headers from the teams-for-linux bug report form template: `Reproduction steps`, `Expected Behavior`, `Debug`, `Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?`. The case and wording must match exactly.

## Key Decisions

See docs/decisions/ for full records. Summary: chose Gemini for its free tier and embedding API, Neon for managed pgvector with connection pooling, Cloud Run for serverless Go with fast cold starts.

## Linting

The project uses golangci-lint v1.64.8 with errcheck, govet, staticcheck, gocritic, and other linters configured in `.golangci.yml`. CI runs linting on every PR. See CONTRIBUTING.md for full development setup and code style guidelines.
