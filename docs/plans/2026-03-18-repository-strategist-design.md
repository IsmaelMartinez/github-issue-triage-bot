# Repository Strategist — 3-Month Roadmap Design

Date: 2026-03-18
Status: Proposed
Supersedes: Nothing (extends the existing roadmap, does not replace it)

## Vision

The project evolves from a doc-grounded issue triage bot into a repository strategist: a GitHub App that maintains institutional memory for software projects and uses it to provide strategic intelligence. The triage pipeline remains as one capability among several, but the system's value proposition shifts from "responds to issues" to "understands your project's trajectory and advises on it."

The positioning against the landscape is deliberate. GitHub Agentic Workflows (in technical preview since February 2026) will commoditise operational chores: doc sync, test coverage, CI failure triage, label management. Renovate handles dependency updates. CodeRabbit handles code review. This system occupies the layer above: connecting decisions, trends, upstream changes, and issue patterns into strategic recommendations that no stateless agent can produce.

The tagline: "Institutional memory and strategic intelligence for GitHub repositories."

## Why This Matters

GitHub's Continuous AI pattern (https://githubnext.com/projects/continuous-ai/) describes a future where repositories host fleets of small, single-purpose agents that run continuously. Most of these agents are stateless — they read the repo, do a thing, and forget. That works for operational tasks but fails for strategic ones.

Strategic intelligence requires three things that stateless agents lack. First, decision memory: knowing that ADR-007 rejected WebRTC because of NAT traversal complexity, and that 12 issues have referenced that ADR in the last month. Second, accumulated learning: remembering that the maintainer rejected three suggestions about Electron autoUpdater because the project uses a custom update mechanism. Third, temporal awareness: noticing that an upstream dependency just shipped a feature that unblocks a deferred roadmap item.

The existing system already has the foundation for all three: a vector store of ADRs, roadmap items, troubleshooting docs, and upstream release notes; a feedback tracking system that captures maintainer signals; and a shadow repo approval pattern that builds trust incrementally. The 3-month roadmap strengthens this foundation and adds the reasoning layer on top.

## Architecture Evolution

### Current State

The current architecture has three main paths. Webhook events come in, the triage pipeline processes them (phases 1, 2, 4a), and results go out as comments via shadow repos. The enhancement research agent handles a parallel path for enhancement issues. The vector store sits alongside as a search backend. The dashboard provides observability.

### New Architectural Concepts

Two new concepts are added on top of everything that exists today.

**Event Journal.** Every significant repo event (issue opened/closed, PR merged, label changed, release published, dependency updated) gets recorded as a lightweight entry in a new `repo_events` table. This is not the vector store — it's a structured log of "what happened" that enables temporal queries. The webhook handler already sees most of these events; the journal persists them in queryable form instead of discarding after processing. For events the webhook doesn't see (releases, dependency changes), a scheduled GitHub Action runs daily, calls the GitHub API, and POSTs a batch of events to a new `/ingest` endpoint.

**Synthesis Engine.** A new `internal/synthesis/` package that periodically reads from both the event journal and the vector store, runs pattern detection, and produces briefings. Briefings are structured documents posted as GitHub issues in the shadow repo for maintainer review. The synthesis engine asks questions like: "Are there issue clusters forming? Does a recent PR contradict an existing ADR? Has an upstream dependency shipped something that unblocks a deferred roadmap item? Is the project's actual direction drifting from the stated roadmap?"

### Multi-Repo Model

The system is deployed as a single GitHub App instance. A new repo adopts it by: installing the GitHub App, adding a `.github/butler.yml` config file, seeding their initial docs into the vector store, and waiting. The butler starts accumulating institutional memory from day one.

The `repo_events` table is keyed by repo. The synthesis engine runs per-repo on its configured schedule. The per-repo config controls which capabilities are enabled, what thresholds to use, doc paths to watch, and upstream dependencies to track. Repos without a config file get no butler behaviour — the App installation alone doesn't activate anything.

### Agent Mix

Some capabilities are doc-grounded (vector store queries + LLM reasoning), others are purely event-driven (pattern matching over the event journal). The system uses whichever is appropriate for each task, not a uniform approach.

## Infrastructure Constraints

Everything must stay within free tiers:

- GCP Cloud Run: 2 million requests/month, 360,000 GB-seconds compute
- Neon PostgreSQL: 0.5 GB storage, 100 hours compute/month
- Gemini 2.5 Flash: 250 requests/day generation, embedding API free
- GitHub Actions: unlimited minutes for public repos
- GitHub API: 5,000 requests/hour per installation

### Storage Budget

The Neon free tier provides 0.5 GB (512 MB). The existing vector store (~406 documents with 768-dim embeddings) consumes roughly 50-60 MB. The issues table (~1,400 rows with embeddings) consumes roughly 20-30 MB. Existing tables (bot_comments, webhook_deliveries, agent_sessions, audit_log, feedback_signals, triage_sessions, approval_gates) are lightweight — estimated 5-10 MB total. That leaves approximately 400 MB for new tables.

The `repo_events` table stores small rows (~300-500 bytes each including JSONB metadata). At 10 events per issue and 1,400 existing issues, plus ongoing activity of roughly 50-100 events per week, steady-state is approximately 20,000-30,000 rows per repo — roughly 10-15 MB per repo. The `doc_references` table is even smaller: roughly 5-10 references per document, 500 documents, yields ~5,000 rows at ~100 bytes each — under 1 MB.

To stay safely within limits: events older than 180 days are archived (soft-deleted with a periodic cleanup job, same pattern as webhook_deliveries). At 3-5 repos, total new storage is roughly 50-80 MB — well within budget. The dashboard should display current storage usage as a health metric.

### Gemini Call Budget

The 250 requests/day limit is shared between the existing triage pipeline and the new synthesis engine. The triage pipeline consumes roughly 3-5 calls per issue (Phase 2 generation, Phase 4a generation, LLM safety review, research synthesis if enhancement). On a busy day with 10 new issues, that's 30-50 calls.

The global budget allocation is: triage gets priority (it's latency-sensitive, triggered by real-time webhook events), synthesis gets the remainder. The synthesis cron should run at a low-traffic time (early morning UTC). The `max_daily_llm_calls` per-repo setting applies to synthesis calls only — triage is never throttled by it. If the global daily quota is exhausted, synthesis degrades gracefully: it runs the event-driven synthesizers (cluster detection via SQL, roadmap staleness via SQL) but skips the LLM-dependent ones (drift detection embedding, briefing generation) and posts a partial briefing noting the skipped sections.

At steady state with 1-2 repos, the budget is: ~50 calls/day triage + ~15 calls/week synthesis = comfortable margin. At 5 repos, synthesis alone would need ~75 calls/week (15/week/repo), which is fine as long as triage doesn't spike. The system tracks daily call counts in a `llm_usage` counter (in-memory, reset daily) and logs warnings at 80% of the daily limit.

## Month 1: Institutional Memory Strengthening (weeks 1-4)

Month 1 has no new user-facing intelligence. It makes the memory layer rich enough that months 2 and 3 can reason over it.

### Week 1-2: Event Journal

New `repo_events` table (migration 011):

```sql
CREATE TABLE repo_events (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL,
    event_type  TEXT NOT NULL,  -- issue_opened, issue_closed, pr_merged, release_published, label_changed, dependency_updated, push, comment
    source_ref  TEXT,           -- issue number, PR number, commit SHA, release tag
    summary     TEXT NOT NULL,  -- short description
    areas       TEXT[],         -- affected code areas/topics
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_repo_events_repo_type ON repo_events(repo, event_type);
CREATE INDEX idx_repo_events_repo_created ON repo_events(repo, created_at DESC);
```

The webhook handler gets a thin layer that writes journal entries for events it already receives: issue opened/closed/edited, issue comments, push events. A new `/ingest` endpoint accepts batched events from external sources. The `/ingest` endpoint is authenticated via a shared secret (`INGEST_SECRET` env var) in the `Authorization: Bearer` header — the same pattern used by the webhook handler's HMAC verification, but simpler since the caller is our own GitHub Action. The `/synthesize` endpoint uses the same secret.

A scheduled GitHub Action (daily) calls the GitHub API for events the webhook doesn't see: releases published, PRs merged (with changed files list), label changes, milestone updates. It POSTs these to `/ingest` with the `INGEST_SECRET` in the Authorization header.

Files:
- `migrations/011_repo_events.sql`
- `internal/store/events.go` — event journal queries
- `internal/webhook/handler.go` — journal write layer
- `cmd/server/main.go` — `/ingest` endpoint
- `.github/workflows/event-ingest.yml` — daily GitHub API scraper

### Week 3: Automatic Document Ingestion

When a push event includes changes to doc paths (configurable in `butler.yml`, defaulting to `docs/**`, `*.md`, `ADR-*`), the system automatically re-embeds the changed documents into the vector store. This requires extracting the core embedding and document upsert logic from `cmd/seed/main.go` into a shared `internal/ingest/` package that both the seed CLI and the server can import. The seed CLI becomes a thin wrapper around this package. The webhook handler calls the same package to embed changed files inline.

For upstream dependencies, a weekly GitHub Action checks for new releases of configured dependencies and seeds them automatically.

Files:
- `internal/ingest/embed.go` — shared embedding and document upsert logic (extracted from cmd/seed)
- `cmd/seed/main.go` — refactored to use internal/ingest
- `internal/webhook/handler.go` — doc change detection on push events, calls internal/ingest
- `.github/workflows/upstream-sync.yml` — weekly upstream release check

### Week 4: Cross-Reference Index

New `doc_references` table (migration 012):

```sql
CREATE TABLE doc_references (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL,
    source_type TEXT NOT NULL,  -- document, issue, pr
    source_id   TEXT NOT NULL,  -- doc title, issue number, PR number
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    relationship TEXT NOT NULL, -- references, implements, contradicts, supersedes
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_doc_refs_source ON doc_references(repo, source_type, source_id);
CREATE INDEX idx_doc_refs_target ON doc_references(repo, target_type, target_id);
```

When a document is embedded, the system extracts references using deterministic regex patterns: issue numbers (`#\d+`), ADR names (`ADR-\d+`), and explicit doc-title references (markdown links to files matching known doc paths). This is purely string-based — no LLM calls. Matching "other doc titles" in free text is intentionally not attempted; only explicit structured references are captured. When an issue is triaged, the phase results that already find related documents via vector similarity are persisted as references (relationship type: "similar").

This creates a lightweight knowledge graph that the synthesis engine queries in month 2.

Files:
- `migrations/012_doc_references.sql`
- `internal/store/references.go` — reference extraction and queries
- `internal/store/postgres.go` — reference extraction during document upsert

### Per-Repo Config File

`.github/butler.yml` schema:

```yaml
# Which capabilities are enabled
capabilities:
  triage: true           # existing triage pipeline
  research: true         # existing enhancement research
  synthesis: true        # new: weekly briefings
  auto_ingest: true      # new: auto-embed docs on push

# Document paths to watch for auto-ingestion
doc_paths:
  - "docs/**"
  - "*.md"
  - "ADR-*"

# Upstream dependencies to track
upstream:
  - repo: electron/electron
    doc_type: upstream_release
    track: releases

# Synthesis schedule
synthesis:
  frequency: weekly      # daily or weekly
  day: monday            # for weekly

# Shadow repo for all butler output
shadow_repo: owner/shadow-repo

# Relevance thresholds (override defaults)
thresholds:
  troubleshooting: 0.70
  adr: 0.55
  roadmap: 0.55
  research: 0.55
  configuration: 0.50
  upstream_release: 0.45
  upstream_issue: 0.45

# Cost guardrails
max_daily_llm_calls: 50
```

The system reads this file via the GitHub Contents API on first webhook event per repo (cached with 1-hour TTL). This requires the GitHub App to have `contents: read` permission, which must be added to the App's permission set. Existing installations will need to accept the updated permissions. If the file is missing or the permission is not granted, the system falls back to default config (triage only, no synthesis, no auto-ingest) — the same behaviour as today.

Files:
- `internal/config/butler.go` — config parsing and caching
- `internal/webhook/handler.go` — config loading before event processing

## Month 2: Pattern Detection Engine (weeks 5-8)

### Week 5: Synthesis Engine Skeleton

New `internal/synthesis/` package:

```go
type Finding struct {
    Type     string   // cluster, drift, upstream_signal, staleness
    Severity string   // info, warning, action_needed
    Title    string
    Evidence []string // links to issues, PRs, docs
    Suggestion string
}

type Synthesizer interface {
    Name() string
    Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error)
}
```

A `Runner` iterates over all configured repos on a cron schedule, calls each registered synthesizer, and collects findings. A `Briefing` builder groups findings into a single markdown document and posts it as a shadow repo issue titled `[Briefing] Weekly — 2026-04-20`.

The cron trigger is a GitHub Action that POSTs to a new `/synthesize` endpoint.

Files:
- `internal/synthesis/types.go` — Finding, Synthesizer interface
- `internal/synthesis/runner.go` — per-repo synthesis orchestration
- `internal/synthesis/briefing.go` — markdown briefing builder
- `cmd/server/main.go` — `/synthesize` endpoint
- `.github/workflows/synthesis.yml` — scheduled cron trigger

### Week 6: Issue Cluster Detection

The first synthesizer. It queries the event journal for issues opened in the time window, uses vector similarity on their existing embeddings to find clusters of 3+ issues about the same topic, and checks whether the cluster has matching roadmap/ADR coverage via the cross-reference index.

If an uncovered cluster is found, it produces a finding: "5 issues opened this month mention screen sharing failures. No existing documentation covers this area. Consider investigating whether this warrants a roadmap item."

The clustering uses existing pgvector infrastructure (cosine similarity between issue embeddings, grouped by a threshold). No new dependencies.

Files:
- `internal/synthesis/clusters.go` — issue cluster synthesizer
- `internal/store/events.go` — temporal issue queries

### Week 7: Decision Drift Detection

The second synthesizer. Two patterns:

**ADR contradiction.** When a PR merges that modifies code in an area governed by an ADR, the system compares the PR against the ADR's decision and alternatives sections. The PR representation used for embedding is the PR title + body + list of changed files (already captured in `repo_events.metadata` from the daily GitHub Action ingest). This avoids embedding full diffs (which can be enormous and blow context windows). Low similarity to the ADR's decision section + high similarity to an alternative section is a signal that the code is drifting away from the stated decision. The ADR's "areas" (extracted from its content during cross-reference indexing in month 1) determine which PRs are checked — only PRs touching files in ADR-governed areas are candidates.

**Roadmap staleness.** Roadmap items that haven't had any related activity (issues, PRs, commits touching relevant areas) in the configured window get flagged as potentially stale or silently deprioritised.

Both query the cross-reference index heavily.

Files:
- `internal/synthesis/drift.go` — ADR drift and roadmap staleness synthesizer
- `internal/store/references.go` — cross-reference queries for drift detection

### Week 8: Upstream Impact Analysis

The third synthesizer. When new upstream releases are ingested, it cross-references the release notes against the project's ADRs, open issues, and deferred roadmap items.

Two output types: opportunities ("Electron v40 shipped WebRTC improvements. ADR-007 deferred WebRTC due to NAT traversal — this may warrant revisiting") and risks ("Electron v40 deprecated BrowserWindow.setMenu(). The project uses this in 3 files").

Files:
- `internal/synthesis/upstream.go` — upstream impact synthesizer
- `internal/store/postgres.go` — queries for deferred/rejected roadmap items and ADRs

### Briefing Format

Each briefing is structured as:

```markdown
# [Briefing] Weekly — 2026-04-20

## Emerging Patterns
[Issue cluster findings — what topics are gaining traction, whether they have doc coverage]

## Decision Health
[ADR drift signals — which decisions the code is diverging from]
[Roadmap staleness — which items have gone quiet]

## Upstream Signals
[Opportunities — upstream changes that unblock deferred work]
[Risks — upstream deprecations that affect the project]

## Metrics
[Issue velocity, resolution rate, phase hit rates — carried from existing dashboard]

---
Generated by Repository Strategist. React with feedback or reply to discuss.
```

## Month 3: Strategic Output (weeks 9-12)

### Week 9: Roadmap Task Generator

Builds on issue cluster detection. When a cluster crosses a configurable threshold (default: 5 issues in 30 days with no matching roadmap/ADR coverage), the butler drafts a roadmap task.

The draft includes: a title, a problem statement sourced from the issue cluster (with links to the specific issues), relevant existing docs from the vector store, and a suggested priority based on issue velocity and user sentiment (derived from reactions data already synced).

Posted as a shadow repo issue titled `[Roadmap Proposal] Screen sharing failures need investigation`. The maintainer can:
- `accept` — butler opens a PR adding the item to the roadmap doc
- `revise` — request changes to the draft
- `reject` — discard

This reuses the existing approval gate infrastructure from the enhancement research agent. PR creation on `accept` requires `contents: write` and `pull_requests: write` permissions on the source repo — these are added alongside the `contents: read` permission from month 1. If these permissions are not granted, the butler posts the proposed roadmap addition as a comment on the shadow issue instead, and the maintainer copies it manually.

Files:
- `internal/synthesis/roadmap.go` — roadmap task generator
- `internal/agent/orchestrator.go` — extend approval signals for roadmap proposals
- `internal/github/client.go` — PR creation for accepted roadmap items (graceful fallback to comment if permissions missing)

### Week 10: ADR Lifecycle Management

Two capabilities:

**ADR revision proposals.** When drift detection flags an ADR contradicted by multiple PRs consistently (not a one-off), the butler drafts an ADR revision. It proposes updated decision text based on what the code actually does, references the PRs that drove the drift, and suggests whether this is a revision or a supersession. Posted as `[ADR Revision] ADR-007: Revisit WebRTC decision`.

**ADR gap detection.** When the butler observes a pattern of related issues and PRs in an area with no ADR coverage, it suggests that an architectural decision should be documented. The suggestion includes a draft ADR skeleton populated with context from the issue cluster and relevant PRs. Posted as `[ADR Proposal] Document screen sharing architecture`.

Both go through the shadow repo approval gate.

Files:
- `internal/synthesis/adr.go` — ADR revision and gap detection
- `internal/agent/orchestrator.go` — extend approval signals for ADR proposals

### Week 11: State of the Project Briefing

A monthly synthesis that goes beyond weekly briefings. It combines all data accumulated over the month into a strategic summary:

- Which areas of the codebase are most active
- Which roadmap items are progressing vs stalling
- What the issue trends suggest about user pain points
- How upstream dependencies are evolving relative to the project's needs
- What decisions might need revisiting

The format is a shadow repo issue titled `[State of the Project] April 2026`, structured as flowing prose with links to evidence throughout. Think of it as the brief a project lead would give to their team at a monthly sync, written by the butler.

If evidence is sparse (fewer than 3 total findings across all weekly briefings in the month), the monthly briefing is skipped and a minimal note is posted instead: "[State of the Project] April 2026 — Quiet month. Not enough activity to produce a meaningful briefing. See weekly briefings for details." This avoids generating thin, low-value reports.

Files:
- `internal/synthesis/state.go` — monthly state-of-project synthesizer
- `internal/synthesis/runner.go` — monthly schedule support

### Week 12: Multi-Repo Hardening and Documentation

The final week focuses on making all of this robust for a second repo to adopt.

- Integration tests that verify the full pipeline (event ingestion, journal, synthesis, briefing, shadow issue) using a test repo
- Documentation of the `butler.yml` config schema
- A "getting started" guide: install the App, add the config, seed your initial docs, wait a week, read your first briefing
- Rate limiting and cost guardrails to ensure the synthesis engine stays within Gemini free tier limits across multiple repos
- Dashboard updates to show synthesis findings and briefing history alongside existing triage metrics

Files:
- `internal/synthesis/runner_test.go` — integration tests
- `internal/config/butler_test.go` — config validation tests
- `README.md` — updated with strategist framing and getting-started guide
- `internal/store/report.go` — synthesis metrics for dashboard

## Failure Modes

**Gemini API unavailable.** The triage pipeline already handles this (warns and skips LLM-dependent phases). The synthesis engine follows the same pattern: event-driven synthesizers (cluster detection, roadmap staleness) that use only SQL queries run normally. LLM-dependent synthesizers (drift detection embedding, briefing prose generation) are skipped, and the briefing notes which sections were omitted. The next scheduled run retries.

**Malformed `butler.yml`.** If the YAML is unparseable or uses unknown fields, the system logs a warning and falls back to default config (triage only). A health alert issue is created in the shadow repo: "[Config Error] butler.yml failed validation — using defaults." The config is re-read on the next webhook event (1-hour cache TTL means the fix takes effect within an hour of a push correcting the file).

**Event journal grows too large.** The 180-day retention policy (see Storage Budget section) prevents unbounded growth. A cleanup job runs alongside the existing webhook_deliveries cleanup. If Neon storage approaches the limit despite retention, the system logs an alert and pauses event journal writes (triage continues unaffected). The dashboard displays current storage usage as a health metric.

**Synthesis finds no patterns.** A briefing with no findings in any section is posted as a minimal "quiet week" note rather than an empty template. This is expected in low-activity periods and is not an error. The maintainer should not receive a noisy briefing when nothing meaningful happened.

**Neon compute hours exhausted.** Neon pauses the database when the 100 compute-hour monthly limit is reached. All operations fail until the next billing cycle. The `/health` endpoint detects this (database connectivity check fails). Mitigation: the synthesis queries should be lightweight (indexed lookups, not full table scans), and the system should track cumulative query time. If synthesis queries consume more than 5 hours in a month, reduce synthesis frequency to monthly.

## What's Deliberately Left Out

Automated PR creation for code changes (too risky for a trust-building phase — the butler proposes, humans implement). Direct public-facing output (everything goes through shadow repos until trust is established). Integration with GitHub Agentic Workflows (wait for GA, then consider using them as the execution layer for scheduled crons). Commodity operational agents (doc sync, test coverage, CI triage — GitHub will provide these for free).

## Relationship to Existing Roadmap

The existing roadmap streams (feedback infrastructure, quality evolution, communication) remain relevant and continue in parallel. Specifically:

- Phase F2 (feedback footer) and F3 (learnings system) proceed as planned — they feed into the synthesis engine's accumulated learning capability
- Phase Q1 (revision loop) and Q2 (threshold adjustment) apply to both triage and synthesis outputs
- Phase C1 (user-facing docs) gets reframed around the strategist positioning
- The Stage A to B transition (selective public posting) remains the gating decision for triage comments

The new work does not block or replace any existing stream. It adds a fourth stream: "Strategic Intelligence."

All synthesis output (weekly briefings, roadmap proposals, ADR revisions, state-of-project reports) goes exclusively through the shadow repo, regardless of whether triage comments have transitioned to public posting via Stage A → B. Synthesis output is inherently advisory and needs maintainer review — there is no scenario where it posts directly to the public repo.

## Measuring Success

After 90 days of the full system running:

- Briefing engagement: >50% of weekly briefings receive a maintainer reaction or reply within 48 hours
- Cluster detection precision: >60% of flagged issue clusters are acknowledged as real patterns by the maintainer (not rejected as noise)
- Drift detection precision: >40% of ADR drift signals lead to either an ADR revision or a code correction (false positive rate expected to be high initially)
- Roadmap proposals: at least 2 accepted roadmap proposals in 90 days
- ADR proposals: at least 1 accepted ADR revision or new ADR in 90 days
- State-of-project usefulness: maintainer reads and shares the monthly briefing (qualitative signal)
- Multi-repo readiness: a second repo can onboard with <1 hour of setup time
- Cost: total Gemini API usage stays within free tier across all configured repos

## Sources and Inspiration

- GitHub Continuous AI: https://githubnext.com/projects/continuous-ai/
- GitHub Agentic Workflows: https://github.blog/ai-and-ml/automate-repository-tasks-with-github-agentic-workflows/
- Continuous AI in Practice: https://github.blog/ai-and-ml/generative-ai/continuous-ai-in-practice-what-developers-can-automate-today-with-agentic-ci/
- Renovate's per-repo config model and confidence badges
- CodeRabbit's learnings system pattern
- Google Cloud "Review and Critique" architecture pattern
