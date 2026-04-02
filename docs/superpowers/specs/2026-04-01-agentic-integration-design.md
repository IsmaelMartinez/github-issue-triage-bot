# Agentic Integration Layer — Design Spec

Date: 2026-04-01
Status: Superseded (2026-04-02) — the phased agent approach was replaced by a simpler model: agents are CLAUDE.md files created as needed, placed where the data is. See the "Agents" section in `docs/plans/2026-03-04-roadmap.md`.

## Vision

Add a Claude Code agent layer that sits on top of the triage bot and repo-butler, consuming both via MCP servers and producing higher-value intelligence by combining per-repo depth (triage bot) with portfolio breadth (repo-butler). The agents run headless in GitHub Actions or interactively from the terminal. They follow the existing tiered trust model: autonomous for shadow repo writes, approval-gated for anything public.

This is a hybrid architecture. The triage bot (Go, Cloud Run) and repo-butler (Node.js, GitHub Action) stay exactly as they are. The agent layer lives in repo-butler as a set of Claude Code agent configurations, one directory per agent, each with focused CLAUDE.md instructions and defined MCP connections.

The agent layer may eventually collapse back into the other two projects as you learn which capabilities belong where. Starting with it explicit lets you see what each agent does, measure its value, and then decide whether it stays separate or gets absorbed.

## Architecture

Three layers, matching the existing stack:

```
Layer 3: Agent Intelligence (new, in repo-butler)
  ├── triage-reviewer
  ├── synthesis-analyst
  ├── governance-executor
  └── health-monitor
         │                    │
         ▼                    ▼
Layer 2: Portfolio Orchestrator (repo-butler)
  MCP server (stdio, Phase 7)
  OBSERVE → ASSESS → IDEATE → PROPOSE → REPORT
         │
         ▼
Layer 1: Institutional Memory (triage bot)
  MCP server (new, stdio over HTTP)
  Webhooks → Triage → Synthesis → Shadow Repos
```

Data flows: repo-butler OBSERVE posts to triage bot `/ingest` (existing). Triage bot processes webhooks and runs synthesis (existing). Agent layer reads from both MCP servers, reasons about combined data, takes action (new). Actions flow back through existing approval gates (shadow repo issues, lgtm/reject).

## Triage Bot MCP Server (this repo)

New Go binary at `cmd/mcp/main.go`. Stdio JSON-RPC protocol, zero new dependencies (Go standard library handles JSON and stdio). Connects to the triage bot's HTTP API for data.

### Tools

`get_pending_triage` — Open triage and research shadow issues that haven't received lgtm/reject, with phase results and age. Wraps dashboard data from `internal/store/report.go`.

`get_synthesis_briefing` — Most recent synthesis briefing(s) within a time window. Wraps data behind `/synthesize` and shadow issue content.

`get_health_status` — Health check results for configured repos: confidence trends, stuck sessions, orphaned triage. Wraps `/health-check`.

`get_report_trends` — Structured synthesis findings: clusters, drift signals, upstream impacts. Wraps `/report/trends`.

### Resources

`triage_config` — Current `butler.json` for each configured repo.

`repo_summary` — Issue counts, document counts, recent activity (the data the dashboard displays).

This MCP server fulfils the triage bot's half of the Phase 8 contract on repo-butler's roadmap. The JSON schemas from repo-butler's Phase 6 inform the response shapes.

## Agent Configurations (repo-butler)

### Directory Structure

```
agents/
  common/
    CLAUDE.md          # shared trust model, output format, MCP connections
  triage-reviewer/
    CLAUDE.md          # agent-specific instructions
  synthesis-analyst/
    CLAUDE.md
  governance-executor/
    CLAUDE.md
  health-monitor/
    CLAUDE.md
```

### Common Base (agents/common/CLAUDE.md)

Defines rules every agent follows:

- Tiered trust model: autonomous for shadow repo writes, approval-gated for public actions.
- Output conventions: concise, no fluff, link to evidence.
- MCP connection instructions for both servers.
- Fallback rule: when in doubt, post to shadow repo and let the human decide.

Each agent's CLAUDE.md inherits the common base and adds its specific purpose, data sources, decision logic, and output targets.

### Triage Reviewer

Runs daily after the dashboard workflow. Calls `get_pending_triage` from the triage bot MCP, reads each pending shadow issue, calls `query_portfolio` from repo-butler's MCP for health context. Produces a prioritised digest as a single shadow issue titled `[Daily Digest] YYYY-MM-DD`. Ranks items by age (older = more urgent), signal strength (more phase hits = richer), and repo health tier (Bronze repos need more attention). Does not approve or reject anything.

### Synthesis Analyst

Runs weekly after the Monday synthesis cron completes. Reads the latest briefing via `get_synthesis_briefing`, reads repo-butler's portfolio trends via `get_snapshot_diff`. Writes an enriched interpretation as a reply comment on the briefing shadow issue, connecting per-repo findings to portfolio-level patterns.

### Governance Executor

Runs after repo-butler's IDEATE/PROPOSE phases when proposals are generated. Reads governance proposals, calls `get_report_trends` and `get_pending_triage` from the triage bot to check for relevant intelligence on affected repos. Enriches proposals with per-repo context before they go to the approval gate. Output is a comment on the governance proposal shadow issue.

### Health Monitor

Runs daily alongside the dashboard workflow. Reads `get_health_status` from the triage bot and `get_health_tier` from repo-butler. Flags convergent signals: if both systems flag the same repo, that gets escalated as a `[Health Alert]` shadow issue with combined evidence. Single-source alerts go through existing channels, not escalated.

## Execution Model

### GitHub Actions Integration

Agents run as steps in repo-butler's existing workflows. No new workflows needed.

**Daily workflow** (2am UTC, existing OBSERVE/ASSESS/REPORT). After existing phases, two agent steps run sequentially: health monitor first (fast), then triage reviewer. Both use the `claude` CLI in non-interactive mode (exact flags TBD during implementation — likely `--print` with `--system-prompt` or equivalent), connecting both MCP servers. Triage bot MCP connects over HTTP to Cloud Run. Repo-butler MCP runs in-process over stdio.

**Weekly workflow** (Monday 6am UTC, synthesis cron). After triage bot synthesis completes, a step runs the synthesis analyst. A retry loop on `get_synthesis_briefing` with a "created after today" filter confirms the briefing exists before the agent reads it.

**Post-IDEATE step** (daily workflow). Governance executor runs after PROPOSE, only when IDEATE produced proposals. Skips if no proposals this run.

### Authentication

- `claude` CLI: `ANTHROPIC_API_KEY` as a GitHub Actions secret.
- Triage bot MCP: `INGEST_SECRET` for Cloud Run authentication.
- Repo-butler MCP: no auth (local stdio).
- GitHub token: existing workflow token for posting shadow issues.

### Manual Invocation

Any agent can run locally: `claude --system-prompt agents/triage-reviewer/CLAUDE.md` from the repo-butler checkout, connecting to the triage bot's Cloud Run URL and the local MCP server.

### Cost Control

Each agent invocation is a single Claude API call (one prompt, one response). Agents are readers and summarisers, not multi-turn conversational loops. Haiku for simpler agents (health monitor, triage reviewer), Sonnet for richer reasoning (synthesis analyst, governance executor). Roughly 4-8 calls per day.

## Implementation Sequencing

Each phase is independently shippable and valuable.

### Phase 1: Triage Bot MCP Server (this repo)

Build `cmd/mcp/main.go` with four tools and two resources. Standalone deliverable, useful for any MCP client. Fulfils the triage bot's half of repo-butler's Phase 8 contract. Test locally with `claude mcp add triage-bot go run ./cmd/mcp`.

### Phase 2: Agent Common Base (repo-butler)

Create `agents/` directory structure. Write `agents/common/CLAUDE.md` with shared trust model, output conventions, MCP connection instructions. Can be done in parallel with Phase 1.

### Phase 3: Health Monitor Agent (repo-butler)

Simplest agent. Validates the entire execution model end-to-end: MCP connections, combined reasoning, shadow issue posting from CI, cost expectations. Add GitHub Actions step to the daily workflow.

### Phase 4: Triage Reviewer Agent (repo-butler)

Most immediately useful agent. Reads pending triage, produces daily digest. Replaces manual shadow issue scanning.

### Phase 5: Synthesis Analyst Agent (repo-butler)

Enriches weekly briefings with portfolio context. Add weekly workflow step.

### Phase 6: Governance Executor Agent (repo-butler)

Depends on repo-butler's Phase 5 (Portfolio Governance Engine) producing proposals. Can wait until governance proposals are flowing.

### Phase 7: ADR Lifecycle Agent (repo-butler) — added 2026-04-01

Originally planned as a Go synthesizer (Task 17: `internal/synthesis/adr.go`). Revised after the codebase analysis confirmed that the existing drift detection synthesizer already produces the raw signals ("ADR-007 has been contradicted by 3 PRs"). What's missing is the reasoning layer that turns those signals into concrete revision proposals — and that requires LLM reasoning with richer context than a hardcoded Go prompt can provide. This agent reads drift findings from the triage bot MCP, cross-references with repo-butler's portfolio context, and drafts ADR revision proposals or gap detection proposals as shadow issues.

### Future Phases (after initial agents are validated)

**Research Agent** — replaces `SynthesizeResearch` in the Go codebase. The current implementation is capped at 8192 tokens, hardcodes teams-for-linux architecture, and cannot read source files or recent GitHub activity. An agent that can read `src/`, check upstream API docs, and browse PR history produces substantially richer research. The Go state machine in `handler.go` stays; the agent replaces just the synthesis call. Absorbs Phase Q1 (revision loop becomes trivial when the agent iterates on its own output).

**Learnings Integration** — the synthesis analyst agent gains the ability to read maintainer feedback signals (rejections, corrections, lgtm patterns) from the MCP server and incorporate them into its weekly analysis. Absorbs Phase F3 (learnings system). The database table for learnings may still be needed in the triage bot, but the reasoning over them belongs here.

**Threshold Advisor** — the synthesis analyst agent reads quality trends from `/report/trends` and recommends approval threshold adjustments. Absorbs Phase Q2 (progressive threshold adjustment).

Dependencies: Phase 1 blocks Phases 3-7. Phase 2 can parallel Phase 1. Phases 3-7 are sequential for validation, not hard dependencies. Future phases depend on having enough data from the initial agents.

## Explicit Non-Goals

**No replacement of existing deterministic functionality.** The triage bot's webhook handling, real-time triage phases, vector search, synthesis algorithms, and safety gates stay as Go code. These are latency-sensitive, well-bounded, and deterministic. Repo-butler's six-phase pipeline stays as-is. Agents are additive.

**No new Go code for LLM-heavy reasoning.** The codebase analysis (2026-04-01) established that LLM reasoning tasks (research synthesis, ADR revision proposals, learnings, quality loops) belong in the agent layer where they benefit from richer context, code navigation, and iterative reasoning. The triage bot's Go code stays focused on data processing and exposure.

**No new database tables or persistent state for agents.** Agents are stateless. They read from MCP servers and write to GitHub. To determine what they've already processed, they read shadow repo issue history.

**No changes to trust model or approval gates.** Agents post to shadow repos like everything else. The lgtm/reject flow doesn't change.

**No multi-agent coordination.** The agents don't talk to each other. If one agent's output should feed another, it happens through the shadow repo (the second agent reads the first's comments). Simple, observable, debuggable.

**No migration of existing features.** The triage bot's scheduled workflows (daily maintenance, weekly synthesis, event ingest) continue as GitHub Actions hitting Cloud Run. Agents are a parallel track, not a replacement.
