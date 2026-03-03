# Consolidated Design: Security Hardening, Data, Dashboard, GitHub App, and Cutover

Date: 2026-03-03

## Context

The triage bot is deployed on Cloud Run and validated against the test repository (IsmaelMartinez/triage-bot-test-repo). All four triage phases produce quality output. The current deployment runs against the test repo with data from teams-for-linux.

A security review identified 12 issues (2 critical, 6 high, 3 medium, 1 low). The database holds only 111 of the 1,356 available issues. There is no public dashboard, and the bot still uses a PAT rather than a GitHub App for authentication. All of these need to be addressed before cutting over to production on teams-for-linux.

This document replaces the previous planning documents (next-phase-design.md, next-phase-implementation.md) as the single source of truth for remaining work.


## Strategy

Everything gets proven on triage-bot-test-repo first. We start with a baseline session that documents the bot's current behavior, then work through 8 batches. Each batch deploys to Cloud Run and validates on the test repo before moving on. Production cutover to teams-for-linux is the final batch and only happens once the full system is working end-to-end on the test repo.

Work is organized into mixed batches: quick mechanical fixes are grouped together, larger refactors get their own session. Each batch ends with validation against the test repo.


## Batch 0: Baseline Validation

**Purpose:** Establish a reference point for measuring improvement and detecting regression.

Create a set of test issues on triage-bot-test-repo that exercise each phase: a bug report with missing sections (Phase 1), a bug matching known troubleshooting docs (Phase 2), a duplicate of an existing issue (Phase 3), an enhancement request (Phase 4a), and a misclassified issue (Phase 4b). Document the bot's current responses as the baseline.

**Validation:** Record each bot comment verbatim. This becomes the comparison reference for all subsequent batches.


## Batch 1: Quick Security Fixes

**Purpose:** Fix all small, mechanical security issues that don't require architectural decisions.

Changes:
- Move Gemini API key from URL query parameter to `x-goog-api-key` request header (`internal/llm/client.go`)
- Add `http.MaxBytesReader` body size limit to webhook handler (`internal/webhook/handler.go`)
- Add non-root USER directive to Dockerfile
- Cap error response body reads with `io.LimitReader` (`internal/llm/client.go`, `internal/github/client.go`)
- Replace AfterConnect DDL hook with `pgvector.RegisterTypes` (`internal/store/postgres.go`)
- Add embedding dimension validation in upsert functions (`internal/store/postgres.go`)
- Run `go mod tidy` to clean stale go.sum entries

**Validation:** Deploy to Cloud Run. Rerun the Batch 0 baseline test issues. Confirm identical or equivalent bot behavior (no regression).


## Batch 2: Secret Manager Migration

**Purpose:** Move secrets from plaintext Cloud Run env vars to Google Secret Manager.

Changes:
- Create Secret Manager secrets for DATABASE_URL, GEMINI_API_KEY, GITHUB_TOKEN, WEBHOOK_SECRET
- Grant the Cloud Run service account secretAccessor role on each secret
- Replace plain `env` blocks with `value_source.secret_key_ref` in the Cloud Run Terraform config
- Remove billing account ID default from Terraform variables

All changes are in `terraform/main.tf`.

**Validation:** `terraform plan` shows expected changes. `terraform apply` succeeds. Create a test issue on triage-bot-test-repo and confirm the bot still responds.


## Batch 3: Prompt Injection Defenses

**Purpose:** Structurally separate trusted LLM instructions from untrusted issue content, and sanitize LLM output before posting to GitHub.

Changes:
- Add `systemInstruction` field support to Gemini API request structure (`internal/llm/client.go`)
- Update `GenerateJSON` to accept separate system and user content parameters
- Restructure all phase prompts: instructions go into systemInstruction, issue content goes into user content (`internal/phases/phase2.go`, `phase3.go`, `phase4a.go`, `phase4b.go`)
- Add LLM output sanitization helper that strips dangerous markdown patterns (`internal/comment/builder.go`)

**Validation:** Deploy. Rerun baseline test issues. Output should be functionally equivalent to Batch 0 but generated through the safer prompt structure. Verify by checking Cloud Run logs that systemInstruction is being used.


## Batch 4: Webhook Replay Protection and CI Hardening

**Purpose:** Prevent webhook replay attacks and harden the CI/CD supply chain.

Changes:
- Add `webhook_deliveries` table migration tracking delivery IDs (`migrations/002_webhook_deliveries.sql`)
- Check and record `X-GitHub-Delivery` header before processing (`internal/webhook/handler.go`)
- Reject duplicate deliveries with 200 OK
- Pin all GitHub Actions to commit SHAs in `.github/workflows/deploy.yml`

**Validation:** Deploy. Send the same webhook payload twice (or create an issue and manually re-deliver via GitHub webhook settings). Confirm the second delivery is rejected. Verify CI workflow still runs with SHA-pinned actions.


## Batch 5: Data Seeding

**Purpose:** Populate the database with the full issue history and feature index for maximum triage quality.

Changes:
- Create issue export tool (`cmd/export-issues/main.go`)
- Add rate limiting to seed CLI (`cmd/seed/main.go`)
- Export all 1,356 issues from teams-for-linux via GitHub API
- Seed all issues into the database
- Regenerate and seed the feature index from docs-site content
- Update ivfflat index lists parameter for larger dataset (`migrations/003_update_ivfflat_lists.sql`)
- Remove the export tool after seeding (it's a one-time utility)

**Validation:** Create test issues on triage-bot-test-repo that should match old teams-for-linux issues (e.g. screen sharing on Wayland, custom notification sounds). Verify Phase 3 duplicate detection finds relevant matches from the full 1,356-issue dataset. Verify Phase 4a returns context from the feature index for enhancement requests.


## Batch 6: Dashboard

**Purpose:** Build public visibility into bot activity and feedback.

Changes:
- Add dashboard stats query methods (`internal/store/report.go`)
- Add `/report` JSON endpoint to the server (`cmd/server/main.go`)
- Create static dashboard HTML generator (`cmd/dashboard/main.go`, `cmd/dashboard/template.html`)
- Create reaction sync tool (`cmd/sync-reactions/main.go`)
- Add store methods for reaction sync and listing bot comments (`internal/store/postgres.go`)
- Add GitHub Actions workflow for daily dashboard generation (`.github/workflows/dashboard.yml`)

**Validation:** Deploy. Hit `/report` endpoint on Cloud Run, verify JSON response. Generate the dashboard locally, review the HTML. Run the reaction sync tool and verify it updates counts.


## Batch 7: GitHub App Conversion

**Purpose:** Replace PAT-based authentication with a proper GitHub App identity.

Changes:
- Register a GitHub App with Issues read/write permission
- Add `bradleyfalzon/ghinstallation` dependency
- Refactor GitHub client from PAT to installation access tokens (`internal/github/client.go`)
- Store the app private key in Secret Manager
- Update Terraform for the new secret and remove GITHUB_TOKEN env var
- Update webhook handler to extract installation_id from payload (`internal/webhook/handler.go`)

**Validation:** Clean switch on triage-bot-test-repo: remove the old PAT webhook, install the GitHub App, create test issues, verify the bot responds through the App identity (should show the App's name/avatar on comments rather than the PAT user's).


## Batch 8: Production Cutover

**Purpose:** Go live on teams-for-linux.

Steps:
- Install the GitHub App on IsmaelMartinez/teams-for-linux
- Create a test issue, verify the bot responds correctly
- Close and delete the test issue
- Disable old triage bot workflows in teams-for-linux (keep for one week as fallback)
- Update all documentation (remaining-work.md, CLAUDE.md, README)

**Validation:** The bot is live and triaging real issues on teams-for-linux through the GitHub App.


## Superseded Documents

This plan supersedes:
- `docs/plans/2026-03-03-next-phase-design.md` (design rationale is preserved here)
- `docs/plans/2026-03-03-next-phase-implementation.md` (task details will be in the implementation plan)
- Security hardening tasks 15-26 from the previous implementation plan (incorporated into Batches 1-4)
