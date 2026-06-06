# ADR-008: Bespoke service vs. a composition of tools and a skill

Status: Resolved by ADR-009 (2026-06-05) — decision is to retire the service and re-home the useful slice
Date: 2026-05-29

## Context

This repository started as an issue-triage bot and has been through a long sequence of pivots: dark factory, lean bot (Phase 3/4b removal, ADR 004), repository strategist, silent RAG mode (ADR 014), and skill-driven triage. Every one of those pivots did the same structural thing — it removed capability from the Go service and moved the reasoning to the agent layer. The maintainer-facing reasoning now lives entirely in the `teams-for-linux-issue-review` Claude Code skill, which calls the service only for retrieval context.

Stripped to essentials, the service today is four things: a webhook listener that embeds every new issue into pgvector on receipt; a `/brief-preview` retrieval endpoint returning a bundle (similar past issues, relevant docs, regression PRs, upstream candidates, learnings); a weekly synthesis engine (issue clustering, decision-drift detection, upstream-impact analysis); and the surrounding infrastructure (event journal, dashboard, health monitor, MCP server, Terraform, Cloud Run, Neon). The reasoning and drafting that the project was originally about no longer happen here.

Two empirical signals from the 2026-05 work motivate this ADR. First, real usage is approximately zero: `/brief-preview` has been hit only by verification calls, never by a real triage of a live issue, and the "retire the Go phases after 30 real-use skill invocations" milestone sits at 0/30. Second, the one capability that genuinely needs a persistent index — semantic search over the ~1,400-issue corpus — is also the one the skill's own step 1c notes flag as unreliable: its neighbours behave like simili-bot's did (keyword-adjacent, mechanism-wrong) and are often beaten by a plain `gh issue list --search` for catching same-week regressions.

The question this raises, and which the maintainer has posed directly: can this bespoke service be replaced by a composition of off-the-shelf tools (the maintainer named Sourcebot as an example) plus the existing Claude skill, leaving only whatever genuinely cannot be assembled that way?

## The core observation: the service has bifurcated

The service's capabilities split cleanly into two kinds, and the split is the heart of this decision.

The first kind is "gather what is in the repo right now" — docs, code, and issues. This has become commodity. The docs corpus (ADRs, roadmap, research) is a few dozen small files that fit in an agent's context window, so RAG over it is over-engineering; an agent simply reads `docs/`. Code search is exactly what tools like Sourcebot, Copilot indexing, and others do well. Issue lookup by keyword is free via `gh search`. This entire half of the service is increasingly redundant with the agent plus generic tooling.

The second kind is "remember and connect things across time" — the event journal and the synthesis engine that detects clusters forming, ADRs drifting from merged PRs, and upstream releases unblocking deferred work. This is genuinely service-shaped and genuinely hard to buy: an agent cannot reconstruct "what patterns formed over the last month" from scratch, and no off-the-shelf tool does longitudinal institutional memory for a single repo. This is the differentiator the roadmap always claimed. The catch is that it, too, is currently unconsumed — findings land in `/report/trends`, which repo-butler was meant to read and largely does not.

So if the project's value is the first kind, the service should dissolve into tools plus the skill. If it is the second kind, the service should shrink to a synthesis-only core and shed all the retrieval scaffolding. Either way the current shape is wrong; the disagreement is only about which half survives.

## Options under consideration

1. Dissolve the service. The skill reads `docs/` and source directly, uses `gh search` for issues, reads a committed `learnings.md` and the existing `hats.md`, and a small GitHub Action handles the Electron upstream watch. Optionally bolt on Sourcebot for portfolio-wide code search. No Cloud Run, no Neon, no webhooks. Loses semantic issue search and temporal synthesis; loses almost nothing that is used today.

2. Collapse to a synthesis-only service. Keep the event journal and the weekly cluster/drift/upstream analysis — the one defensible, hard-to-buy capability — and delete the retrieval endpoint, the dead agent/mirror code, the dashboard, and the health monitor. The skill does its own gathering; the service only does longitudinal memory.

3. Keep the service as-is, justified only if real usage materializes. Status quo.

## Decision gate

No option is chosen yet. The deciding input is usage, not architecture taste, and usage is currently zero — which by default argues for option 1. The non-sunk-cost path is: now that the skill's curl fallback reliably reaches `/brief-preview`, use the skill for real on the next 10–20 live issues and measure one thing — did the retrieval bundle change a reply the maintainer would otherwise have written without it? If yes a meaningful fraction of the time, the irreducible core has been found and option 2 is built around it. If not, option 1 is the answer.

## Specifics still to evaluate (to be deepened)

This section is intentionally a scaffold; the next working session will turn each line into a grounded finding.

- Capability inventory and usage audit: enumerate every endpoint and `internal/` package, classify each as used / unused / dead, and map each to (a) a commodity tool, (b) "the agent can do it directly," or (c) "genuinely bespoke."
- The semantic-issue-search question: is semantic search over the issue corpus worth keeping at all given the observed noise, or does `gh search` plus the agent suffice? If kept, does it need this service or could a lighter index serve?
- Cost and maintenance ledger: current Cloud Run + Neon + CI + Terraform run-cost and the maintenance burden (this session alone fixed a doc drift, a dead metric, an embedding leak, and a misrouted MCP registration on an unused service), versus the footprint of each alternative.
- Data preservation: is the pgvector corpus worth exporting/keeping, and what is the migration/rollback path for each option.
- Multi-repo ambition: is portfolio-wide institutional memory still a goal, or has the project effectively become teams-for-linux-only? This reframes everything.
- The repo-butler boundary: what repo-butler already covers, so we do not retire something it depends on or rebuild something it provides.
- Interaction with the deferred rename (roadmap Stream 7): if the service dissolves, the rename question is moot; if it survives as synthesis-only, the rename should reflect the narrower identity.

## Findings (2026-05-29)

Two of the specifics above have been investigated and are now grounded.

Sourcebot does not replace the core; it covers a peripheral slice. Verified against its official docs, GitHub, and changelog: Sourcebot indexes code only (no issues, PRs, discussions, or release notes), searches lexically/regex via Zoekt (no semantic/embedding search — that is a roadmap aspiration, not shipped), offers an agentic "Ask Sourcebot" code Q&A, and exposes an 11-tool MCP server that operates entirely on code, symbols, commits, and diffs. It is multi-repo, self-hosted (Docker plus Postgres and Redis), under a fair-source FSL licence. It does none of triage, duplicate detection, temporal/trend analysis, decision-drift detection, or upstream cross-referencing. Conclusion: Sourcebot (or a tool like it) could replace `internal/codenav` and the general "find relevant source for an issue" need — and would do so better than the bespoke build, with a ready MCP server — but it cannot replace the institutional-memory core (issue/doc/upstream RAG, synthesis). This sharpens the framing: the "gather now" half decomposes cleanly into commodity tools, but the "remember over time" half has no off-the-shelf substitute. The choice is therefore not "swap in tools" but "is the bespoke memory core worth keeping at all."

The synthesis engine runs reliably but its output is barely consumed. The weekly cron has executed nine successful scheduled runs since 2026-03-30, each generating findings. But consumption is near-zero: `/report/trends` — the structured endpoint built specifically for repo-butler to ingest findings, the entire Month-3 integration justification — had zero reads in the last 30 days, so that integration is effectively dead. The findings do surface on the live `/dashboard`, which was viewed roughly seven times in the same window (presumably by the maintainer), plus one `/report` read. So the output is glanced at occasionally by a human but never consumed programmatically, and at "occasional glance" volume rather than "drives decisions."

This leaves one decision-hinging question that logs cannot answer and that only the maintainer can: in those dashboard glances, has a synthesis finding ever actually changed a call — prompted an ADR revision, a roadmap reprioritisation, or caught an upstream unblock that would otherwise have been missed? If yes even a few times, the synthesis sliver has earned its place and option 2 is real. If the glances were curiosity with no action following, then both halves are unused in practice — retrieval (untried, because the skill wiring was broken until 2026-05-29) and synthesis (tried for two months, never acted on) — and option 1 (dissolve) is essentially forced. Note the asymmetry: synthesis has had a fair two-month trial and its consumption failed; retrieval has not had a fair trial yet and is owed the 10–20 live-issue measurement from the decision gate before being judged.

## What we are not deciding yet

We are not committing to retire, rebuild, or migrate anything in this ADR. This records the framing, the options, and the measurement gate so the decision can be made deliberately once the specifics above are filled in and a short real-usage measurement has run.

## References

- Roadmap: `docs/plans/2026-03-04-roadmap.md`
- Silent RAG mode: `docs/adr/014-retire-shadow-repos.md`
- Lean pivot: `docs/decisions/004-lean-bot-pivot.md`
- Skill-driven triage design and pilot: `docs/plans/2026-05-15-skill-driven-triage-design.md`
- simili-bot trial outcome (a prior "buy vs build" call): `docs/decisions/007-simili-bot-trial.md`
- Sourcebot capability profile (2026-05-29 investigation): docs.sourcebot.dev/docs/overview, docs.sourcebot.dev/docs/features/mcp-server, sourcebot.dev/changelog, sourcebot.dev/blog/fair-source
