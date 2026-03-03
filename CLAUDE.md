# CLAUDE.md

## Project Overview

GitHub Issue Triage Bot: a standalone Go service that automatically triages issues on IsmaelMartinez/teams-for-linux by analyzing issue content and posting helpful comments. Deployed as a serverless container on Google Cloud Run, backed by Neon PostgreSQL with pgvector for vector similarity search, and Gemini 2.5 Flash for LLM generation and embeddings.

## Essential Commands

```bash
# Run tests
go test ./...

# Run vet
go vet ./...

# Build server and seed binaries
go build -o server ./cmd/server
go build -o seed ./cmd/seed

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

The service receives GitHub webhook events (issue opened/closed/reopened) and runs a multi-phase triage pipeline:

Phase 1 (pure parsing, no LLM): detects missing information in bug reports by checking form sections against known templates. Phase 2 (pgvector + LLM): embeds the issue, searches the troubleshooting/configuration document store for similar entries, sends top-5 to Gemini to generate solution suggestions. Phase 3 (pgvector + LLM): embeds the issue, searches the issue store for similar past issues, sends top-5 to Gemini to identify potential duplicates. Phase 4a (pgvector + LLM): for enhancement requests, searches roadmap/ADR/research documents for related context. Phase 4b (LLM only): checks if the issue might be misclassified (bug vs enhancement vs question).

All phase results are consolidated into a single markdown comment by the comment builder.

## Project Structure

```
cmd/server/main.go           # HTTP server entry point (/webhook, /health, /report)
cmd/seed/main.go              # CLI to import JSON indexes into database
cmd/dashboard/main.go         # Static dashboard HTML generator
cmd/export-issues/main.go    # One-time CLI to export GitHub issues to JSON
cmd/sync-reactions/main.go   # Sync GitHub reactions to bot comments in DB
internal/webhook/handler.go   # Webhook verification, replay protection, routing
internal/phases/              # Triage phases (phase1.go through phase4b.go)
internal/comment/builder.go   # Consolidates phase results into markdown
internal/comment/sanitize.go  # LLM output and URL sanitization
internal/llm/client.go        # Gemini API client (generation + embeddings)
internal/github/client.go     # GitHub App client (comments, webhook verification)
internal/store/postgres.go    # PostgreSQL + pgvector queries
internal/store/report.go      # Dashboard stats queries
internal/store/models.go      # Shared data types
migrations/                   # Database migrations (001-003)
terraform/main.tf             # GCP infrastructure (Cloud Run, AR, budget, secrets)
.github/workflows/deploy.yml  # CI/CD: test on PR, build+deploy on push to main
.github/workflows/dashboard.yml # Daily dashboard generation + GitHub Pages
docs/decisions/               # Architecture decision records
docs/plans/                   # Implementation plans
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

Environment variables: DATABASE_URL (required), GEMINI_API_KEY (optional, warns if missing), GITHUB_APP_ID (required, numeric App ID), GITHUB_PRIVATE_KEY (required, base64-encoded or raw PEM), WEBHOOK_SECRET (required), SOURCE_REPO (optional, overrides repo for vector searches), PORT (optional, defaults to 8080).

## Issue Template Headers

Phase 1 parses these exact section headers from the teams-for-linux bug report form template: `Reproduction steps`, `Expected Behavior`, `Debug`, `Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?`. The case and wording must match exactly.

## Key Decisions

See docs/decisions/ for full records. Summary: chose Gemini for its free tier and embedding API, Neon for managed pgvector with connection pooling, Cloud Run for serverless Go with fast cold starts.
