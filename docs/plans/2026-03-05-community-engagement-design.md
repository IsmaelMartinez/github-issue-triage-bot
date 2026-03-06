# Community Engagement Design: Maintainer-First Triage

Date: 2026-03-05
Status: Implemented (Stage A active)

## Current State (2026-03-06)

The bot is deployed and running in Stage A (invisible helper) on both teams-for-linux and the test repo. Silent mode has been removed (PR #30). Shadow repos handle all gating: `IsmaelMartinez/teams-for-linux` → `IsmaelMartinez/teams-for-linux-shadow`, `IsmaelMartinez/triage-bot-test-repo` → `IsmaelMartinez/triage-bot-test-repo-shadow`.

Track 1 (bug triage via shadow repo) is live. New bug issues get a `[Triage]` shadow issue with the full triage comment. `lgtm` promotes to the public issue; `reject` closes the session. Verified end-to-end on test repo (shadow issues #6, #11).

Track 2 (enhancement context brief) is live after PR #32 (context brief flow) and PR #33 (skip LLM safety check for briefs). New enhancement issues get a `[Research]` shadow issue with a context brief. `research` triggers full Gemini synthesis, `use as context` acknowledges and closes, `reject` closes. Verified end-to-end on test repo (shadow issues #8, #9, #10).

One real issue has been processed on teams-for-linux: enhancement #2304 (spellcheck) arrived before the context brief code was deployed, so it went through the old full-research flow. Bug issues #2300, #2296, #2293, #2292 were processed while silent mode was still active, so no shadow issues were created for them.

Dashboard gap: the dashboard does not yet surface agent session outcomes (lgtm/reject/research/use-as-context rates). The data is in `agent_sessions` and `agent_audit_log` tables but needs queries and UI. This is needed before Stage B graduation decisions can be data-driven.

## Problem

The triage bot has a full pipeline (5 phases), an Enhancement Researcher agent, safety layers, and a dashboard, but it's in silent mode after negative community reactions. Even Phase 1 (pure template parsing) was perceived as "bad AI." The bot needs to become useful to the maintainer first, then gradually earn community trust through demonstrated value.

## Design Principles

Build for full transparency (Stage C), deploy as invisible helper (Stage A), graduate through selective public presence (Stage B) when the data supports it. The community only sees the bot when the maintainer has validated its output.

## Two Tracks

### Track 1: Bug Triage via Shadow Repo

No changes to the triage pipeline. Phases 1 through 4b run exactly as today. The routing change: disable silent mode and let the existing shadow repo path activate.

When a bug issue is opened on teams-for-linux, the bot creates a shadow issue titled `[Triage] #N: Original title` with the original issue body, then posts the triage comment as the first reply with `lgtm` / `reject` instructions. The maintainer reviews via GitHub notifications on the shadow repo.

`lgtm` promotes the comment to the public issue. `reject` closes the shadow issue silently. This path is already implemented and requires only a deployment config change: `SILENT_MODE=false` with `SHADOW_REPOS` configured.

### Track 2: Enhancement Context Brief

When an enhancement issue is opened, the agent creates a shadow issue as today. Instead of launching into the full research state machine, it posts a context brief as the first comment.

The context brief has three sections:

Request summary: two or three Gemini-generated sentences describing what the enhancement asks for and why it matters. This is the only LLM synthesis, kept intentionally small.

Related context: the actual content (not just links) from vector search results, organized by type. Related ADRs with title, status, key decision, and relevant excerpt. Roadmap items with title, status, last updated, and description. Past issues with number, title, state, resolution if closed, and one-line summary. Research documents with title and summary. Each item includes a one-sentence Gemini-generated relevance note. Up to 3-5 items per category.

Footer with three action signals: `research` triggers the full Gemini research synthesis (the existing pipeline), `reject` closes the session, `use as context` acknowledges the brief is being taken to Claude and closes the session cleanly.

The state machine adds one new stage: NEW -> CONTEXT_BRIEF_POSTED -> COMPLETE for the default path. The `research` signal branches into the existing flow: CONTEXT_BRIEF_POSTED -> RESEARCHING -> REVIEW_PENDING -> APPROVED/COMPLETE, preserving all revision, PR creation, and publish logic.

### Rationale for Context Brief over Full Research

Gemini has the ADRs, roadmap, and knowledge base but lacks repository context, so its research synthesis is generic. The bot's genuine value is the vector search infrastructure (1,356 issues, 18 troubleshooting docs, ADRs, roadmap items all embedded). The context brief leverages that strength. The maintainer takes the structured context to Claude (which has the repo) for deep investigation when needed. A Gemini synthesis that's "almost right" is negative value because it anchors thinking in the wrong direction.

The full research pipeline is preserved as opt-in via the `research` signal for cases where Gemini is good enough (simple enhancements, well-documented areas).

## Graduation Path

### Stage A: Invisible Helper (now)

Silent mode off, shadow repos configured. The maintainer is the only audience. Bug triage and enhancement briefs arrive as shadow issues. The community sees nothing. The dashboard tracks which phases produce useful output and whether the lgtm rate justifies going public.

### Stage B: Selective Public Presence (when ready)

The maintainer promotes triage comments via `lgtm` on phases that work well. The community sees the bot's comment on their public issue with the existing bot disclosure footer. Phase 1 is the likely first candidate (deterministic, enforces the maintainer's own template rules). The community experiences a bot that's helpful when it shows up, because bad outputs have been filtered.

### Stage C: Full Transparency (when confident)

Re-enable direct public posting for phases with high lgtm rates. Add the feedback footer (roadmap F2). Make the dashboard public. The shadow repo remains active for enhancement briefs and for phases still being gated. The community can see the dashboard, react to comments, and submit feedback via the issue template. The transition from B to C is a configuration change plus the feedback footer, no architectural changes.

## Implementation Scope

### Completed (PRs #30, #32, #33)

`internal/agent/handler.go`: StartSession rewritten to post a context brief instead of entering the clarifying/research flow. `handleContextBriefResponse` handles research/use-as-context/reject signals. `askClarifyingQuestions` removed (unused after rewrite). LLM safety check skipped for context briefs (structural check only) because briefs intentionally include diverse vector search results.

`internal/agent/research.go`: `BuildContextBrief` assembles vector search results with an LLM-generated summary, partitioning documents by type (ADR/roadmap/research). `FormatContextBriefMarkdown` renders conditional sections with action signal footer.

`internal/store/models.go`: `StageContextBrief` constant added.

`internal/agent/orchestrator.go`: `SignalResearch` and `SignalUseAsContext` signals added with parsing logic.

Deployment: silent mode removed (PR #30), shadow repos configured for both teams-for-linux and test repo.

### What stayed the same

The triage pipeline (all phases), comment builder, safety layers, shadow repo infrastructure, dashboard, reaction sync, the full research/revision/PR/publish flow (accessible via research signal), and all approval signal parsing. The bug triage path had zero code changes.

### Outstanding

The dashboard does not surface agent session metrics. Adding lgtm/reject/research/use-as-context rates to the dashboard is needed for data-driven Stage B graduation.

## Measuring Success

Track lgtm vs reject rate on shadow issues per phase. A phase is ready for Stage B when the maintainer lgtm's >80% of its output. A phase is ready for Stage C when it's been in Stage B for 30+ days with positive or neutral community reactions (no "bad AI" signals).

For enhancement context briefs, measure how often the maintainer uses `research` (Gemini synthesis is useful) vs `use as context` (taking it to Claude) vs `reject` (brief wasn't helpful). This informs whether to invest in improving Gemini synthesis or doubling down on the context brief format.
