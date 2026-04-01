# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly. Do not open a public GitHub issue.

Email: ismael@ismaelmartinez.me.uk

Include as much detail as possible: the affected component, steps to reproduce, and the potential impact. You should receive a response within 72 hours acknowledging receipt.

## Supported Versions

Only the latest version deployed to Cloud Run is supported with security updates. There are no versioned releases yet (planned for v0.1.0).

## Security Architecture

The bot processes GitHub webhook events and calls the Gemini API. Its security posture is designed around these trust boundaries.

### Authentication and Secrets

Webhook payloads are verified using HMAC-SHA256 with a shared secret (`WEBHOOK_SECRET`). The verification uses constant-time comparison and reads the full body before verification to prevent timing attacks. Replay protection tracks delivery IDs in the database.

The GitHub App private key (`GITHUB_PRIVATE_KEY`) authenticates the bot to the GitHub API. It is stored as a GitHub Actions secret and in GCP Secret Manager, never in the codebase. Installation tokens are short-lived (1 hour) and scoped to the installed repositories.

Database credentials (`DATABASE_URL`) and the Gemini API key (`GEMINI_API_KEY`) are also stored in GCP Secret Manager and injected at runtime via Cloud Run environment variables.

### LLM Security

All LLM-generated output passes through two safety layers before being posted to GitHub:

1. A structural validator (`internal/safety/structural.go`) enforces length limits, blocks `@mentions`, detects control characters, and checks URLs against a hostname allowlist. This is deterministic and fast.

2. An LLM reviewer (`internal/safety/llm_validator.go`) checks for relevance, reflected prompt injection, inappropriate tone, scope creep, and harmful content. It runs at low temperature (0.1) and fails closed (any error returns `Passed=false`).

Both layers must pass before content reaches a shadow repo, and again before content is promoted to a public issue.

### Infrastructure

The service runs on Google Cloud Run with a non-root container user. The Docker image uses a multi-stage build with Alpine Linux. CI/CD uses Workload Identity Federation (no long-lived service account keys). Terraform state is in GCS with versioning and locking.

### Code Execution

The codebase has a single `exec.Command` call in `internal/mirror/mirror.go` for git operations. All arguments are hardcoded git subcommands or validated repository URLs. No user-controlled input reaches command construction.

All SQL queries use parameterised statements via pgx. There is no string concatenation in production SQL.

## Dependencies

The project uses minimal external dependencies: pgx/v5 (PostgreSQL), pgvector-go, and ghinstallation/v2 (GitHub App auth). Go module checksums are verified via `go.sum`. Dependabot monitoring is enabled for Go modules and GitHub Actions.
