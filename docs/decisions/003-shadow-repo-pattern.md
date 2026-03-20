# ADR-003: Shadow repo pattern for staging bot output

**Status:** Accepted
**Date:** 2026-03-05

## Context

The bot was put in silent mode (ADR 002) after negative community reactions, but storing drafts in a database table gave maintainers no practical review workflow. The Enhancement Researcher agent also needed a private conversation space where maintainers could interact with the bot without exposing unreviewed output to the public.

## Decision

Route all bot output — triage comments and agent sessions — through mirror issues in a private shadow repository. Maintainers review output via comment signals (`lgtm`/`reject` for triage, `research`/`use as context`/`reject` for enhancements) before anything reaches the public issue. The shadow repo mapping is configured via the `SHADOW_REPOS` environment variable as comma-separated `owner/repo:owner/shadow` pairs.

## Consequences

This introduced the `internal/mirror/` package for mirror issue creation and `internal/agent/orchestrator.go` for signal parsing. All bot output now passes through a human review gate before reaching users, which prevents bad output from being visible but adds latency between triage completion and public comment posting. The pattern also established the convention of `[Triage]` and `[Research]` prefixed shadow issues that the dashboard and health monitor rely on.

## References

- `docs/plans/2026-03-05-shadow-triage.md`
- `docs/plans/2026-03-05-community-engagement-design.md`
