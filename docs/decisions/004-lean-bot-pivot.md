# ADR-004: Lean bot pivot — removing Phases 3 and 4b

**Status:** Accepted
**Date:** 2026-03-15

## Context

Phase 3 (duplicate detection) and Phase 4b (misclassification detection) overlapped significantly with GitHub's native duplicate detection and label management features. They each required an LLM call, adding cost and latency without providing differentiated value. The bot's real strength lies in doc-grounded intelligence from the vector store, not in replicating platform features.

## Decision

Remove Phases 3 and 4b entirely from the triage pipeline. Focus the bot on Phase 1 (template parsing, no LLM), Phase 2 (doc-grounded suggestions across all document types via vector search and LLM), and Phase 4a (enhancement context from roadmap, ADR, and research docs). This narrows the bot's scope to capabilities that cannot be replicated by GitHub's built-in tooling.

## Consequences

LLM calls per triage dropped from 4 to 2, cutting both cost and latency. The pipeline became simpler and produced fewer false positives, particularly from duplicate matching which had been unreliable on small issue corpora. Phase 2 was broadened during this pivot to search all 5 document types (troubleshooting, configuration, ADR, roadmap, research) with per-category relevance thresholds (troubleshooting 70%, ADR/roadmap/research 55%, configuration 50%).

## References

- `docs/plans/2026-03-15-lean-bot-pivot-design.md`
