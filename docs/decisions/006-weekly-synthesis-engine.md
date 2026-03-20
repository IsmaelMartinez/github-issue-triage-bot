# ADR-006: Weekly synthesis engine for institutional memory

**Status:** Accepted
**Date:** 2026-03-18

## Context

The bot was purely reactive — it only acted when issues were opened or commented on. Maintainers had no way to see cross-cutting patterns, drifting architectural decisions, or upstream dependency impacts without manually reviewing issues and documentation. This limited the bot's value as a repository strategist that maintains institutional memory.

## Decision

Build a synthesis engine with three weekly analysers: issue cluster detection (groups similar recent issues by embedding cosine similarity), decision drift detection (flags ADRs contradicted by merged PRs and stale roadmap items), and upstream impact analysis (cross-references new dependency releases against existing project docs). Findings are aggregated and posted as a `[Briefing]` shadow issue for maintainer review.

## Consequences

This added the `internal/synthesis/` package with cluster, drift, and upstream analysers, triggered weekly via a `/synthesize` endpoint called by a cron workflow. All findings go through the shadow repo pattern (ADR 003) before reaching maintainers. The engine depends on the event journal (`repo_events` table) for temporal analysis of repository activity, which was introduced as migration 011. This is the first proactive capability in the bot, shifting it from a reactive triage tool toward the repository strategist vision.

## References

- `docs/plans/2026-03-18-repository-strategist-design.md`
- `docs/plans/2026-03-18-repository-strategist-implementation.md`
