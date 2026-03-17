# ADR 012: Dashboard Consolidation — Live Endpoint Only

## Status

Implemented

## Context

ADR 011 introduced a static HTML dashboard generated daily and deployed to GitHub Pages. When the live `/dashboard` endpoint was added later (Dashboard v2), both outputs rendered the same template with the same data. The static dashboard added maintenance cost (a daily cron job, a separate binary, GitHub Pages deployment) without providing a different view or audience. Users and maintainers preferred the live endpoint because it showed current data.

## Decision

Remove the static dashboard generator (`cmd/dashboard`) and consolidate on the live `/dashboard` endpoint served by the Cloud Run service. Enhance the live dashboard with Chart.js time-series charts, clickable drill-down into individual sessions, and auto-refresh. The daily GitHub Actions workflow is simplified to only run stale session cleanup, health checks, and reaction sync.

## Consequences

### Positive

One dashboard to maintain instead of two. The live dashboard shows real-time data with charts and drill-down that the static version could not support. The daily workflow is simpler and no longer needs GitHub Pages permissions.

### Negative

No offline/cached dashboard if the Cloud Run service is down. This is acceptable given the low traffic and the existing health check monitoring.

## Related

- Supersedes: ADR 011
- Live dashboard: `cmd/server/dashboard.go` + `cmd/server/template.html`
- Daily workflow: `.github/workflows/dashboard.yml`
