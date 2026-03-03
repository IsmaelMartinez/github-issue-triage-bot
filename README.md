# GitHub Issue Triage Bot

An automated issue triage assistant for the [Teams for Linux](https://github.com/IsmaelMartinez/teams-for-linux) project. When a new issue is opened, the bot analyzes its content and posts a helpful comment with relevant context: known solutions from documentation, potential duplicates from issue history, related roadmap items, and missing information prompts.

## How it works

The bot runs as a Go service on Google Cloud Run, receiving GitHub webhook events. When an issue is opened, it runs a multi-phase triage pipeline:

- Phase 1 checks if the bug report is missing key information (reproduction steps, debug logs, expected behavior) by parsing the issue body against the project's form template.
- Phase 2 searches the troubleshooting documentation using vector similarity (pgvector) to find known solutions, then uses Gemini to generate targeted suggestions with links.
- Phase 3 searches past issues for potential duplicates, again using vector similarity followed by LLM-based semantic comparison.
- Phase 4a (enhancements only) searches roadmap items, architecture decisions, and research documents to surface existing context about similar feature requests.
- Phase 4b checks whether the issue might be miscategorized (e.g., a question labeled as a bug).

All phase results are consolidated into a single markdown comment. The bot identifies itself as automated and notes that a maintainer will review.

## Architecture

```
GitHub Webhook (issue events)
        |
        v
Cloud Run (Go binary)
        |
        +-- Phase 1: Template parsing (no LLM)
        +-- Phase 2: pgvector search + Gemini (bugs)
        +-- Phase 3: pgvector search + Gemini (bugs)
        +-- Phase 4a: pgvector search + Gemini (enhancements)
        +-- Phase 4b: Gemini classification (all)
        |
        v
Post consolidated comment via GitHub API
        |
        v
Neon PostgreSQL + pgvector
(documents, issues, bot_comments)
```

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

### Environment variables

The server requires `DATABASE_URL`, `GITHUB_APP_ID`, `GITHUB_PRIVATE_KEY`, and `WEBHOOK_SECRET`. `GEMINI_API_KEY` is optional (the bot logs a warning and skips LLM phases if unset). `GITHUB_PRIVATE_KEY` should contain the PEM content either as raw PEM text or base64-encoded PEM. Two optional variables control runtime behavior: `SOURCE_REPO` overrides the repository used for vector similarity searches (useful when testing against a different repo than the one sending webhooks), and `PORT` sets the HTTP listen port (defaults to 8080).

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
```

After the initial seed, the webhook handler keeps issue data up to date in real-time.

## Infrastructure

Terraform manages the GCP resources (Cloud Run, Artifact Registry, billing budget). State is stored in a GCS bucket with versioning and locking. The database is Neon PostgreSQL with pgvector, managed outside Terraform.

See `docs/decisions/` for architecture decision records and `docs/plans/` for implementation plans.

## Installing the GitHub App

To use this bot on your own repository:

1. Register a GitHub App at https://github.com/settings/apps/new with permissions: Issues (read & write). Subscribe to the "Issues" webhook event. Set the webhook URL to your Cloud Run service URL + `/webhook`.
2. Generate and download a private key PEM file from the App settings.
3. Install the App on the target repository.
4. Set the environment variables: `GITHUB_APP_ID` (numeric App ID from settings), `GITHUB_PRIVATE_KEY` (base64-encoded PEM or raw PEM content), and `WEBHOOK_SECRET` (the secret you configured when creating the App).
5. Deploy via `terraform apply` or set the secrets in your CI/CD environment.
