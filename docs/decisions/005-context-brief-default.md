# ADR-005: Context brief as default agent response

**Status:** Accepted
**Date:** 2026-03-05

## Context

The Enhancement Researcher agent originally ran a full research synthesis pipeline by default, involving multiple LLM calls, revision cycles, and PR creation. This was heavyweight for most enhancement issues where the maintainer simply needed quick context about related ADRs, roadmap items, and similar past issues rather than a complete research report.

## Decision

Default to a lightweight context brief that formats vector search results as markdown with minimal LLM usage. The full Gemini synthesis pipeline is preserved but only triggered when a maintainer explicitly posts the `research` signal. A separate `use as context` signal lets the maintainer acknowledge the brief and close the session when taking the context to external tools.

## Consequences

Most agent sessions now complete in a single step instead of multiple round-trips, significantly reducing LLM cost per enhancement issue. The full research pipeline remains available on demand for issues that warrant deeper analysis. This two-tier approach also simplified the approval flow — context briefs need only `use as context` or `reject`, while the heavier research flow adds the `research` trigger and subsequent `lgtm`/`revise` review cycle.

## References

- `docs/plans/2026-03-05-enhancement-context-brief.md`
