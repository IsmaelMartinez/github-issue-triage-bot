# Community Engagement Design: Maintainer-First Triage

Date: 2026-03-05
Status: Approved

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

### Changes required

`internal/agent/handler.go`: modify StartSession to post a context brief instead of entering the clarifying/research flow by default. New function to assemble the context brief using existing vector search helpers.

`internal/agent/research.go`: new function to build the context brief template (request summary + related context sections). Reuses existing vector search calls.

`internal/store/models.go`: new stage constant StageContextBrief.

`internal/agent/orchestrator.go`: recognize `research` signal to branch from StageContextBrief into the existing research flow. Recognize `use as context` to complete the session.

Deployment config: set SILENT_MODE=false (or remove the silent mode check entirely since shadow repos handle the gating).

### What stays the same

The triage pipeline (all phases), comment builder, safety layers, shadow repo infrastructure, dashboard, reaction sync, the full research/revision/PR/publish flow (accessible via research signal), and all approval signal parsing. The bug triage path has zero code changes.

### What gets removed

Nothing. The existing research pipeline becomes opt-in rather than default, but no code is deleted.

## Measuring Success

Track lgtm vs reject rate on shadow issues per phase. A phase is ready for Stage B when the maintainer lgtm's >80% of its output. A phase is ready for Stage C when it's been in Stage B for 30+ days with positive or neutral community reactions (no "bad AI" signals).

For enhancement context briefs, measure how often the maintainer uses `research` (Gemini synthesis is useful) vs `use as context` (taking it to Claude) vs `reject` (brief wasn't helpful). This informs whether to invest in improving Gemini synthesis or doubling down on the context brief format.
