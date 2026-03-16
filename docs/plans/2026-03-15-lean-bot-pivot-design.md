# Lean Bot Pivot: Remove Commodity Phases, Focus on Doc-Grounded Intelligence

Date: 2026-03-15

## Context

After comparing with GitHub's native AI triage, trIAge, OpenClaw (210k+ stars), and simili-bot, we identified that duplicate detection (Phase 3) and misclassification labelling (Phase 4b) are commodity features now available from multiple tools. However, Phases 1, 2, and 4a are differentiated because they search a project-specific vector store of troubleshooting guides, configuration options, ADRs, and roadmap items. No existing tool replicates this doc-grounded intelligence.

## What Changes

### Remove

Phase 3 (duplicate detection): currently embeds the issue, searches the issue store for similar past issues, and asks the LLM to identify duplicates. This is well-served by simili-bot (semantic duplicate detection with vector search) or GitHub's emerging native capabilities. Removing it eliminates one LLM call and one vector search per triage.

Phase 4b (misclassification detection): currently asks the LLM whether an issue is mislabelled (bug vs enhancement vs question). GitHub's native AI triage and basic Copilot integrations handle label suggestions. Removing it eliminates one LLM call per triage.

### Keep

Phase 1 (missing information detection): pure string parsing against the teams-for-linux bug report template. No LLM cost, project-specific, and no external tool replicates checking against your exact template headers. Zero reason to remove.

Phase 2 (troubleshooting/config doc matching): embeds the issue, searches the vector store for matching troubleshooting and configuration documents, and surfaces relevant known solutions. This is the core differentiator for bug triage — it connects user-reported problems to your project's actual documentation. No existing tool does this.

Phase 4a (enhancement context from ADRs/roadmap): searches the vector store for related architecture decisions, roadmap items, and research documents. Feeds into the enhancement research agent. Unique to this project.

Enhancement research agent: context briefs, full research synthesis, shadow repo review gating. The most differentiated feature of the entire system.

Shadow repo pattern: private maintainer review before public posting. Unique workflow.

Dashboard: live metrics at /dashboard. Unique.

Vector store: 1,362 issues, 18 troubleshooting docs, 31 ADR/research/roadmap documents. Powers Phases 2, 4a, and the research agent. This is the knowledge layer that makes the bot valuable.

## Code Changes

### Files to modify

`internal/webhook/handler.go` — remove Phase 3 and Phase 4b calls from both `handleOpened` (the main triage pipeline) and `handleRetriage` (the /retriage command), which independently calls Phase 3 and Phase 4b. Also clean up `collectPhasesRun` to remove the dead Phase3/Phase4b branches. After removal, the bug triage pipeline becomes: Phase 1 (parse) → Phase 2 (doc match) → build comment.

`internal/webhook/handler_test.go` — update `TestCollectPhasesRun` which constructs `comment.TriageResult` values with Phase3 and Phase4b fields.

`internal/comment/builder.go` — remove the Phase 3 (duplicate suggestions) and Phase 4b (misclassification hint) sections from the comment builder. Clean up the `hasContent` guard which includes `len(r.Phase3) > 0` and `r.Phase4b != nil` conditions. The triage comment becomes simpler: greeting, Phase 2 known-issue matches, Phase 1 missing-info checklist, debug instructions, tip link, bot disclosure. Note: if Phase 4b was the only content-producing phase for an issue (e.g. a non-bug non-enhancement), the comment will now be empty where it previously wasn't. This is acceptable since such cases are rare and the label suggestion was the weakest output.

`internal/comment/builder_test.go` — remove test cases for Phase 3 duplicate rendering and Phase 4b misclassification rendering.

`cmd/backfill/main.go` — remove Phase 3 and Phase 4b calls. This file independently calls both phases and builds a TriageResult with their fields. Must be updated or the backfill command will fail to compile when phase files are deleted.

Dashboard template and `internal/store/report.go` require no changes. The template renders phases dynamically from JSON (`Object.keys(phr).sort()`), and the queries use `unnest(phases_run)` which will naturally stop showing Phase 3 and 4b once they stop being recorded. Historical data remains visible until it ages out.

### Files to delete (optional, can defer)

`internal/phases/phase3.go` — duplicate detection. Can be deleted entirely or kept as dead code for reference.

`internal/phases/phase4b.go` — misclassification detection. Same.

`internal/phases/phase3_test.go` — if it exists.

### Files unchanged

`internal/phases/phase1.go` — pure parsing, no changes.

`internal/phases/phase2.go` — doc matching, no changes.

`internal/phases/phase4a.go` — enhancement context, no changes.

`internal/agent/` — research agent, no changes.

`internal/store/postgres.go` — vector store queries, no changes.

`internal/safety/` — safety validators, no changes.

## Migration Plan

### Step 1: Enable GitHub native triage (manual, no code change)

Enable GitHub's AI triage on IsmaelMartinez/teams-for-linux via repository settings. This handles label suggestions and basic classification. Verify it works for a few issues before proceeding.

Also evaluate whether to install simili-bot or a similar tool for duplicate detection, or rely on GitHub's native capabilities as they mature.

### Step 2: Remove Phase 3 and 4b from the pipeline

Modify the webhook handler to skip Phase 3 and Phase 4b calls. Update the comment builder to remove their output sections. Update tests. This is a single PR.

The `phases_run` array in triage sessions will stop including "phase3" and "phase4b" for new triages. Historical data remains unchanged.

### Step 3: Clean up dead code (optional)

Delete phase3.go, phase4b.go, and their tests. Remove the Duplicate and Misclassification types from types.go. Remove the Phase3 and Phase4b fields from TriageResult in builder.go.

This can be deferred if we want the option to re-enable them. But they add maintenance weight (keeping them compiling, updating when shared code changes), so deleting is cleaner.

### Step 4: Update documentation

Update CLAUDE.md project description to reflect the new scope: "a doc-grounded triage and enhancement research bot" rather than "a multi-phase triage pipeline." Update the roadmap. Update the bot's comment disclosure text if needed.

## Impact on Dashboard

The dashboard's Phase Breakdown and Phase Hit Rate sections will stop showing Phase 3 and 4b for new triages. Historical data will still show them. This is fine — the bars simply won't appear for new data.

The triage summary cards (total, promoted, pending) are unaffected since they track sessions, not phases.

## Impact on Bot Output

Bug triage comments become shorter and more focused. Instead of 4 sections (known issues, duplicates, missing info, misclassification hint), they have 2-3 sections (known issues, missing info, debug instructions). This is actually an improvement — the duplicate and misclassification sections were identified as the weakest output in the quality review.

Enhancement flow is completely unaffected.

## Risk Assessment

Low risk. Phases 3 and 4b are independent — they read from the issue but don't affect other phases. Removing them is purely subtractive. The comment builder already handles missing phase data gracefully (empty arrays produce no output).

The only risk is removing duplicate detection before GitHub native or simili-bot adequately replaces it. Mitigate by enabling the replacement tool first (Step 1) and verifying it works before removing our implementation (Step 2).

## Out of Scope

Replacing the entire triage pipeline with OpenClaw or trIAge. Our doc-grounded phases (1, 2, 4a) are differentiated and worth keeping. Only the commodity phases (3, 4b) are being removed.

Changing the enhancement research agent. It stays exactly as-is.

Changing the shadow repo pattern. It stays exactly as-is.
