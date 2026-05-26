# Webhook Catch-Up

Date: 2026-05-26
Status: Draft

## Problem

The bot embeds issues via webhook. If a delivery fails or is missed, those issues stay unembedded and degrade similarity search. GitHub reliability is ~99%+, but gaps accumulate over months.

## Approach

A weekly GitHub Actions cron job runs a modified `cmd/backfill` to embed any issues missing from the `issues` table. Follows the established pattern of `dashboard.yml` (daily) and `upstream-refresh.yml` (monthly).

## Backfill Modifications

The current `cmd/backfill` only fetches closed issues and runs the full triage pipeline. Three changes needed: generalize to fetch all issue states (open + closed), add a store method `ExistingIssueNumbers(ctx, repo)` to skip already-embedded issues, and add a `--mode=catchup` flag that bypasses the triage pipeline and only calls `UpsertIssue` (embed-only, matching the silent RAG webhook path).

## Idempotency

`UpsertIssue` uses `ON CONFLICT (repo, number) DO UPDATE`, so re-processing is harmless. The skip-if-exists check is a performance optimization (avoids redundant Gemini embedding calls), not a correctness requirement.

## Workflow

New file: `.github/workflows/webhook-catchup.yml`. Schedule: `cron: '0 6 * * 0'` (Sunday 06:00 UTC). Same auth/secrets pattern as existing workflows. Invokes `go run ./cmd/backfill --mode=catchup`. Rate limiting: existing 1-second sleep between issues, `BACKFILL_LIMIT=50` default.

## Files Affected

- `cmd/backfill/main.go` — add catchup mode, generalize state filter, skip-if-exists
- `internal/store/postgres.go` — add `ExistingIssueNumbers` method
- `.github/workflows/webhook-catchup.yml` — new workflow
