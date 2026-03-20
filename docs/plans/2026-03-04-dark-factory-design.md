> **SUPERSEDED**: The Enhancement Researcher agent was implemented, then simplified by the context brief design (`2026-03-05-enhancement-context-brief.md`) and lean bot pivot (`2026-03-15-lean-bot-pivot-design.md`).

# Dark AI Factory: Enhancement Researcher Agent Design

Date: 2026-03-04

## Context

The triage bot is deployed and operational on teams-for-linux. It receives GitHub webhook events, runs a multi-phase triage pipeline, and posts a single comment on each new issue. All 8 batches from the consolidated plan are complete: security hardening, Secret Manager, prompt injection defenses, webhook replay protection, data seeding, dashboard, GitHub App conversion, and production cutover.

The next evolution is turning this reactive commenter into an agentic system -- a "dark AI factory" where issues and enhancements from users feed back into the system. The bot doesn't just comment; it researches, asks questions, produces design documents, and eventually implements fixes. This document covers the first slice: the Enhancement Researcher agent.

## Vision

The long-term goal is a system where enhancement requests trigger autonomous research, validation, and eventually implementation. Bug reports trigger reproduction, diagnosis, and auto-fix attempts. The system operates with progressive autonomy: initially every step requires human approval, graduating to milestone-only approval, then confidence-based autonomy, and finally budget-gated autonomy with escalation.

This design covers the Enhancement Researcher -- the first concrete capability. It exercises the core agent loop (receive event, research, converse, produce artifact) without needing code execution, making it the right starting point.

## Scope

This design covers: the agent state machine, webhook extension for conversation handling, the enhancement research pipeline, safety layers, runner abstraction, database schema, and the shadow repo pattern for private iteration.

Out of scope for now: bug auto-fixer, implementation agent, code execution backends (GitHub Actions runner, Cloud Run Jobs runner, Claude Code runner), confidence-based or budget-gated approval modes.

## Architecture Decisions

The system extends the existing Go monolith rather than introducing a separate orchestrator service. The webhook handler, Gemini client, pgvector store, and GitHub client are all reused. The agent logic is new code within the same deployment. When the agent work becomes too heavy for Cloud Run (likely when we add the implementation agent with code execution), we extract it into a separate service. The runner abstraction is designed as an interface from the start to make this extraction straightforward.

Target repositories are curated via an allowlist, matching the existing `allowedRepos` pattern in the server. The GitHub App is installed on each allowed repo and its corresponding shadow repo.

## Agent State Machine

Each enhancement issue triggers an `AgentSession` that progresses through stages:

```
NEW -> CLARIFYING -> RESEARCHING -> REVIEW_PENDING -> APPROVED -> COMPLETE
                                         |
                                      REVISION
                                         |
                                    RESEARCHING (loop back)
```

The session is stored in a new `agent_sessions` table with columns for repo, issue number, shadow repo, shadow issue number, current stage, context (a JSONB blob holding accumulated research, clarifying Q&A pairs, confidence scores), round-trip count, and timestamps.

A conversation counter tracks round-trips with the user. The target is 2 (great) to 4 (acceptable). If the system hits 4 round-trips without reaching REVIEW_PENDING, it escalates to the maintainer with a summary of what it knows and what it's stuck on.

The approval model starts as "approve at every stage." The bot posts its output and waits for an explicit approval signal (a reaction like thumbs-up, or a keyword like "approved" / "lgtm") before advancing. The approval checker is an interface so we can swap in confidence-based and budget-gated modes later without changing the state machine.

## Webhook Extension

The handler currently processes `issues` events only. To support the conversation loop, it also handles `issue_comment` events.

When an `issue_comment` event arrives with action `created`, the handler looks up `agent_sessions` for that repo+issue. If a session exists and the comment isn't from the bot itself, it resumes the session's state machine with the new comment as input. If no session exists, the comment is ignored.

For the initial enhancement trigger, `handleOpened` stays as-is for bugs. For enhancements, after running the standard triage phases and posting the public comment, it creates an `AgentSession` in the NEW stage and kicks off the research pipeline in the shadow repo.

The `X-GitHub-Event` check broadens from `"issues"` to also accept `"issue_comment"`. Webhook signature verification, dedup, and body size limits apply identically to both event types. The GitHub App needs the `issue_comment` webhook event subscription enabled in its settings.

## Shadow Repo Pattern

All agent conversation happens in a private shadow repo, not on the public issue. This lets the maintainer iterate with the bot without public scrutiny during early development.

The flow:

1. Enhancement arrives on public repo. Bot posts standard triage comment (public, as today).
2. Bot creates a mirror issue in the shadow repo with a link back to the original.
3. All agent conversation (clarifying questions, research posting, approvals) happens on the shadow issue.
4. Research PR targets the shadow repo's `docs/research/` directory.
5. When the maintainer approves the final output, the bot posts a curated summary on the original public issue and optionally opens a PR on the public repo.

The promotion step is a new approval gate type: `promote_to_public`. The maintainer triggers it by commenting "publish" or "promote" on the shadow issue.

Each public repo has a configured `shadow_repo` in the allowlist. The GitHub App must be installed on both repos. The agent session tracks both `source_repo` (public) and `shadow_repo` (private).

## Enhancement Research Pipeline

Three steps mapping to the state machine stages.

In CLARIFYING, the bot analyzes the enhancement body and decides if it has enough information to research. It looks for a clear description of the desired behavior, the problem being solved, and any constraints or preferences. If any of these are vague, it posts specific questions on the shadow issue (multiple choice where possible). The LLM generates questions using a system prompt that includes the repo's existing docs/ADRs as context. If the body is already detailed enough, this stage is skipped and the session jumps to RESEARCHING.

In RESEARCHING, the bot does three things. It searches the pgvector store for related documents (ADRs, roadmap items, existing research) and similar past issues, reusing the existing Phase 4a and Phase 3 logic. It asks the LLM to synthesize a research document: a structured markdown covering what the user wants, how it relates to existing work, 2-3 potential implementation approaches with trade-offs, open questions, and a recommendation. It stores the research document in the documents table (doc_type "research") with an embedding for future retrieval.

In REVIEW_PENDING, the bot posts the research document as a comment on the shadow issue and asks the maintainer to approve, request revisions, or reject. If revisions are requested, the session moves to REVISION, the bot incorporates the feedback, and loops back to RESEARCHING.

In COMPLETE, the bot creates a branch, commits the research document as `docs/research/YYYY-MM-DD-<slug>.md`, opens a PR on the shadow repo, and links it back to the shadow issue. The research document becomes a permanent part of the repo's knowledge base, searchable via both pgvector embeddings and the repo's file tree.

## Safety Layers

Two layers: an outer structural layer and an inner LLM validation layer.

The structural layer runs on every piece of content before it gets posted as a comment or committed to a file. It enforces: maximum comment length, no executable code blocks in research documents unless clearly labeled as examples, no external URLs that weren't in the original issue or the repo's existing docs, no @-mentions of users not already involved in the issue thread, and character-set restrictions (no control characters, no zero-width spaces).

The LLM validation layer takes the bot's draft output and runs it through a separate Gemini call with a reviewer system prompt. The reviewer checks: does the response address the issue, does it contain reflected prompt injection, is the tone appropriate, does it stay within scope. The reviewer returns pass/fail with a confidence score. On failure, the output is discarded and the bot either retries or escalates.

Both layers are behind interfaces for extensibility. Structural rules can be configured per-repo. The LLM reviewer prompt is stored as a document in the repo so maintainers can customize what "appropriate" means.

## Runner Abstraction

The runner interface decouples agent tasks from their execution environment:

```go
type Runner interface {
    Execute(ctx context.Context, task Task) (Result, error)
}
```

The first implementation is `InProcessRunner` which runs tasks in goroutines within the Cloud Run service, matching the existing `wg.Add(1)` pattern. Future implementations: `GitHubActionsRunner` (dispatches workflows via API, polls for completion), `CloudRunJobRunner` (creates Cloud Run jobs), `ClaudeCodeRunner` (starts Claude Code sessions via API).

For the enhancement researcher, all tasks are LLM-based, so `InProcessRunner` is adequate. The runner abstraction becomes critical when the implementation agent needs to check out code, run tests, and create PRs.

## Database Schema

Three new tables.

`agent_sessions` tracks the state machine: repo, issue_number, shadow_repo, shadow_issue_number, stage (text enum), context (JSONB), round_trip_count, created_at, updated_at. Primary key on (repo, issue_number). The context blob grows as the session progresses.

`agent_audit_log` records every agent action: session_id, action_type (asked_question, posted_research, created_pr, escalated), input_hash, output_summary, safety_check_passed (bool), confidence_score, created_at. This is the accountability trail.

`approval_gates` tracks pending approvals: session_id, gate_type (clarification_approval, research_approval, pr_approval, promote_to_public), status (pending, approved, rejected, revision_requested), approver, created_at, resolved_at.

No changes to existing tables. Research documents go in the existing `documents` table with doc_type "research".

## Progressive Approval Model

The approval system is designed to evolve through four modes:

Mode 1 (initial): Approve everything. Every stage transition requires explicit human approval on the shadow issue.

Mode 2: Milestone approval. Human approves the research document and the promotion to public. Clarifying questions and research iteration happen autonomously.

Mode 3: Confidence-based. The system self-assesses confidence at each gate. Below a configurable threshold: pause and ask. Above: proceed. Human sets the threshold per repo.

Mode 4: Budget-gated autonomy. Per-issue budget (time, API calls, Actions minutes). System operates freely within budget. Escalates when budget runs low or confidence is low.

The approval checker interface supports all four modes. The mode is configured per-repo in the allowlist. Switching modes is a config change, not a code change.

## Future Extensions

Once the enhancement researcher is working, the next slices in priority order:

1. Bug auto-fixer: reproduce, diagnose, open fix PR. Requires code execution (GitHub Actions runner).
2. Implementation agent: takes approved research documents and splits them into implementation tasks. Requires Claude Code runner.
3. Validation agent: runs spikes on research ideas before they reach REVIEW_PENDING. Adds a VALIDATING stage to the state machine.
4. Orchestration agent: coordinates multiple agents working on related issues. Manages dependencies and resource allocation.

Each extension adds new stages to the state machine and new runner implementations, but the core architecture (sessions, approval gates, safety layers, shadow repo) stays the same.
