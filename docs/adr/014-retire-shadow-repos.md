# ADR 014: Retire Shadow Repos — Silent RAG Mode

## Status

Implemented

## Context

The shadow repo mechanism (ADR in `docs/plans/2026-03-05-shadow-triage.md`) was designed as an approval gate: bot-generated triage comments were posted to a private shadow repo where the maintainer could review them and reply `lgtm` to promote the comment to the public issue, or `reject` to discard. The intent was quality control — preventing bad triage from reaching reporters.

After three months of operation, a four-agent audit revealed that the shadow mechanism had become a bottleneck rather than a quality gate. 75% of shadow issues (30 of 40) auto-closed after 14 days without any maintainer review. Only 7 received `lgtm` and were promoted. The maintainer was not reviewing shadow issues fast enough to keep up, and the overhead of checking a separate repo for each incoming issue was not sustainable for a single-maintainer project.

Meanwhile, the `/teams-for-linux-issue-review` Claude Code skill became the primary triage interface. The skill calls the bot's `get_brief_preview` MCP endpoint, which queries the vector store directly for similar issues, relevant docs, upstream candidates, and regression PRs. This bypasses the shadow mechanism entirely — the skill gets RAG context without any shadow posting or review gate. The skill's output quality was validated in the skill-driven-triage evaluation (May 2026) against live issues, not shadow issues.

The embedding pipeline (`upsertIssue` at handler.go:417) and the shadow posting pipeline (handler.go:531+) were already cleanly separated in the code. Removing shadow posting had no impact on the RAG retrieval that the skill depends on.

## Decision

Remove shadow posting entirely. Switch the webhook handler to silent RAG mode: embed every incoming issue into the vector store and return immediately, without running the triage pipeline (Phase 1/2/synthesis) or posting any comments. The full retrieval pipeline only runs on demand when the skill calls `/brief-preview`.

Archive three repos: `triage-bot-test-repo`, `triage-bot-test-repo-shadow`, and `teams-for-linux-shadow`.

## Consequences

### Positive

Eliminates per-issue Gemini API costs for the triage pipeline (Phase 1 JSON extraction, Phase 2 LLM scoring, synthesis generation). Only the embedding call runs per webhook. Removes the maintainer bottleneck — no shadow repo to check. Simplifies the codebase by removing shadow posting, the `/retriage` command, the promote flow, and shadow-specific dashboard metrics. Three repos archived, reducing maintenance surface.

### Negative

No bot-initiated triage comments reach reporters. Users who filed issues previously received automatic bot responses with related issues and troubleshooting links. That feedback loop is now skill-driven and maintainer-initiated rather than automatic. If the maintainer does not run the skill on a new issue, the reporter gets no automated response.

The historical triage session data in the database becomes read-only. Dashboard metrics referencing shadow outcomes (promotion rate, approval gate stats) are removed. Historical data remains queryable via SQL but is no longer visualised.

## Related

- Supersedes: `docs/plans/2026-03-05-shadow-triage.md`
- Spec: `docs/superpowers/specs/2026-05-26-simplification-and-rag-evolution-design.md`
- RAG endpoint preserved: `cmd/server/brief_preview.go`
- Skill consumer: `/teams-for-linux-issue-review` skill via `get_brief_preview` MCP tool
