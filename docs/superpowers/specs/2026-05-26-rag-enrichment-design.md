# RAG Enrichment: Triage Learnings Store

Date: 2026-05-26
Status: Draft

## Context

The bot embeds issues and upstream docs into a vector store for retrieval via `get_brief_preview`. What it lacks is feedback signal — when the maintainer corrects the skill's draft or changes labels the bot misclassified, that correction evaporates. This design adds a `triage_learning` doc type that captures those corrections as searchable RAG context.

## Data Model

A new `DocTypeLearning = "triage_learning"` constant in `models.go`. Each learning is a `Document` row with a deterministic title key (`learning/<issue_number>/<kind>`) for upsert deduplication. Content is a natural-language description of the correction; metadata includes `issue_number`, `kind` (response_diff, label_correction, close_correction), optional `hat`, and `captured_at`. No schema migration needed — `UpsertDocument` handles it via the existing `(repo, doc_type, title)` constraint.

## Capture Mechanism 1: Skill Draft vs. Posted Response

The skill runs in Claude Code, not in the bot. Bridge: a new `POST /learning` endpoint. After the maintainer posts a reply that differs from the skill's draft, the skill's step 8 POSTs the diff summary to the bot. The endpoint embeds the summary and upserts a `triage_learning` document. Authentication uses the webhook secret as a bearer token.

## Capture Mechanism 2: Label and Close Corrections via Webhooks

The webhook handler already processes `labeled`, `unlabeled`, and `closed` events. New logic: when a label change contradicts what Phase 1 classification would have suggested (only for issues where `bot_comments` confirms the bot triaged it), generate a learning. Same for close events where the bot didn't flag the issue for closure. Content is a sentence describing the correction with issue context.

## Retrieval Integration

`briefPreviewHandler` runs a second `FindSimilarDocuments` call filtered to `triage_learning` with limit 3, returned in a new `learnings` field on the response. Hat boost applies. The skill's step 1c consumes the field to factor corrections into future drafts.

## Files Affected

- `internal/store/models.go` — add `DocTypeLearning` constant
- `cmd/server/learning.go` — new `/learning` endpoint (~80 lines)
- `cmd/server/main.go` — register route
- `cmd/server/brief_preview.go` — query and return learnings
- `internal/webhook/handler.go` — detect label/close contradictions, upsert learnings (~60 lines)
- `~/.claude-home/skills/teams-for-linux-issue-review/SKILL.md` — step 8: POST on diff; step 1c: consume learnings

## Out of Scope

Bulk backfill of historical corrections. Automatic decay of old learnings (recency boost handles this). Confidence calibration of learning weight vs docs.
