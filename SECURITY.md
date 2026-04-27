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

### Endpoint Authentication

All mutating endpoints (`/cleanup`, `/health-check`, `/ingest`, `/synthesize`, `/pause`, `/unpause`) require a Bearer token matching the `INGEST_SECRET` environment variable. The service logs a warning at startup if this secret is not set. The `/pause` and `/unpause` endpoints additionally validate the `repo` parameter against the configured allowed repos. Read-only endpoints (`/report`, `/report/trends`, `/dashboard`) serve aggregated data and are publicly accessible.

### Webhook Security

Webhook payloads require both a valid HMAC-SHA256 signature (`X-Hub-Signature-256`) and a unique delivery ID (`X-GitHub-Delivery`). Payloads without a delivery ID are rejected. Replay protection tracks delivery IDs for 30 days. The webhook body size is limited to 2 MB. Background processing goroutines include panic recovery to prevent a single malformed event from crashing the server.

### Output Sanitisation

Two sanitisation paths protect against malicious LLM output. The triage pipeline (Phase 2, Phase 4a) passes each LLM-generated field through `sanitizeLLMOutput` which strips HTML tags, script elements, dangerous URI schemes, and GFM image syntax including reference-style images (preventing tracking pixel injection). The enhancement research agent additionally runs output through the structural validator (URL hostname allowlist, `@mention` blocking) and the LLM safety reviewer before posting to shadow repos, and again before promoting content to public issues.

### Infrastructure

The service runs on Google Cloud Run with a non-root container user. The Docker image uses a multi-stage build with Alpine Linux. CI/CD uses Workload Identity Federation (no long-lived service account keys). Terraform state is in GCS with versioning and locking.

### Secret Rotation

Each secret has a single source of truth and a redeploy step that propagates the new value to Cloud Run. Three storage locations are kept in sync: GCP Secret Manager (read by Cloud Run at runtime), GitHub Actions repository secrets prefixed `TF_VAR_*` (read by the deploy workflow when running `terraform apply`), and `terraform/terraform.tfvars` (gitignored, used for local `terraform plan/apply` runs).

To rotate the **GitHub App private key**, generate a new key from the GitHub App settings page and download the PEM. Add it as a new version of the `triage-bot-github_private_key` secret in Secret Manager (`gcloud secrets versions add triage-bot-github_private_key --data-file=newkey.pem`). Update the `TF_VAR_GITHUB_PRIVATE_KEY` GitHub Actions secret with the same value (base64-encoded). Update `terraform.tfvars` locally if you keep one. Push any commit to main, or trigger the deploy workflow manually, so Cloud Run picks up the new secret version. Once Cloud Run is serving on the new key, revoke the old key in the GitHub App settings.

To rotate the **webhook secret**, regenerate it in the GitHub App settings, update `triage-bot-webhook_secret` in Secret Manager and `TF_VAR_WEBHOOK_SECRET` in GitHub Actions, and redeploy. There is a small window during the redeploy where in-flight webhook deliveries signed with the old secret will fail HMAC verification; GitHub will retry these automatically.

To rotate the **Gemini API key**, generate a new key in Google AI Studio or the Cloud Console, update `triage-bot-gemini_api_key` and `TF_VAR_GEMINI_API_KEY`, and redeploy. Once Cloud Run logs show successful Gemini calls on the new revision, delete the old key.

To rotate the **database URL**, rotate the Neon project password (which produces a new connection string), update `triage-bot-database_url` and `TF_VAR_DATABASE_URL`, and redeploy. The bot's connection pool will pick up the new credential on the next Cloud Run revision.

The cron-triggered endpoints (`/cleanup`, `/health-check`, `/ingest`, `/synthesize`, `/upstream-watch`) are authenticated via Cloud Run IAM using Workload Identity Federation. The GitHub Actions cron workflows in `dashboard.yml`, `event-ingest.yml`, `synthesis.yml`, and `upstream-watcher.yml` mint short-lived ID tokens through the configured WIF pool and pass them as Bearer tokens. WIF rotation is handled by GCP; there is no shared secret to rotate. The `INGEST_SECRET` environment variable is intentionally left unset in production — it would add an app-layer Bearer check on top of IAM, but the IAM gate is sufficient on its own. Setting it in `terraform.tfvars` would require re-issuing it to every cron workflow; do not introduce it without that follow-through.

For routine hygiene, rotate the GitHub App private key and the webhook secret once a year. Other secrets should be rotated on demand if compromise is suspected, and immediately if a contributor with access leaves the project.

### Code Execution

The codebase has a single `exec.Command` call in `internal/mirror/mirror.go` for git operations. All arguments are hardcoded git subcommands, and repository slugs are validated against a strict `owner/name` regex before use. No user-controlled input reaches command construction.

All SQL queries use parameterised statements via pgx. There is no string concatenation in production SQL.

## Dependencies

The project uses minimal external dependencies: pgx/v5 (PostgreSQL), pgvector-go, and ghinstallation/v2 (GitHub App auth). Go module checksums are verified via `go.sum`. Dependabot monitoring is enabled for Go modules and GitHub Actions.
