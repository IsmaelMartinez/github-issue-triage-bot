# ADR 011: Public Dashboard with GitHub Pages

## Status

Superseded by ADR 012

## Context

Maintainers need visibility into how the bot is performing: how many issues have been triaged, which phases fire most often, and whether comments are helpful. Without a dashboard, the only way to assess the bot is to manually review individual issue comments. Reaction data (thumbs up/down) on bot comments provides a lightweight signal of comment quality, but this data is scattered across individual issues and not aggregated anywhere.

## Decision

Generate a static HTML dashboard daily via a GitHub Actions workflow. The workflow runs on a cron schedule (once per day) with a manual trigger option. It performs three steps: (1) sync reaction data from GitHub by calling the reactions API for all bot comments and storing counts in the database, (2) query the `/report` endpoint on the deployed Cloud Run service for aggregate statistics (issues triaged, phase hit rates, reaction totals), and (3) render a static HTML page from the stats using `cmd/dashboard/main.go` and deploy it to GitHub Pages via `peaceiris/actions-gh-pages`.

The dashboard is a single self-contained HTML file with inline CSS — no JavaScript frameworks, no build step, no external dependencies. It shows total issues triaged, a breakdown by phase, reaction counts, and recent activity.

## Consequences

### Positive

Zero hosting cost since GitHub Pages is free for public repositories. No authentication required for viewing public statistics. The static generation approach means no runtime infrastructure beyond what already exists. The daily cron keeps the dashboard reasonably current without continuous polling.

### Negative

Dashboard data is stale by up to 24 hours between cron runs. The reaction sync step requires a GitHub token with read access to issue reactions, adding a secret to the workflow. The static HTML approach limits interactivity — there are no filters, date range selectors, or drill-downs.

### Neutral

The report endpoint on Cloud Run serves the same data that could power a live dashboard in the future. If real-time visibility becomes important, the endpoint is already available — the static dashboard is a pragmatic starting point that can be replaced without changing the backend.

## Related

- Dashboard generator: `cmd/dashboard/main.go`
- Reaction sync: `cmd/sync-reactions/main.go`
- Report endpoint: `cmd/server/main.go` (`/report` handler)
- GitHub Actions workflow: `.github/workflows/dashboard.yml`
- Report queries: `internal/store/report.go`
