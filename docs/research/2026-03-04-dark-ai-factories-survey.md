# Dark AI Factories: State of the Art Survey

## The "Dark Factory" Concept

The term comes from manufacturing ("lights-out factories"). In software engineering, it was crystallized by a HackerNoon article (Feb 2026) describing four phases of moving from AI-assisted to fully autonomous coding. StrongDM's three-person team published their manifesto at factory.strongdm.ai, covered by Simon Willison.

### The Four Phases

Phase 1 (Context Enhancement): Better AGENTS.md files, build-before-push validation, actionable linter output. Where most teams are today.

Phase 2 (Spec-Driven Development): Engineers submit structured markdown specs. An autonomous agent generates code validated through holdout scenarios — acceptance tests the coding agent never sees. A separate LLM evaluator judges satisfaction on a 0-100 scale, running three times with 2-of-3 pass threshold to mitigate non-determinism.

Phase 3 (Selective Automation): Auto-merging for services above 90% pass rate, below 5% false positives, below 10% human override rate.

Phase 4 (Full Autonomy): Tickets tagged for automation flow through the entire pipeline unattended.

The critical insight is the separation between generation and validation layers. The coding agent never sees the holdout scenarios, preventing specification gaming — this mirrors ML's train/test separation.

StrongDM's "Digital Twin Universe" creates behavioral clones of third-party services (Okta, Jira, Slack) for testing. Their charter: code must not be written or reviewed by humans. Generated code is treated as "opaque weights whose correctness is inferred exclusively from externally observable behavior."

## Real-World Implementations

### OctopusGarden (Open Source Dark Factory)

github.com/foundatron/octopusgarden — Implements the dark factory pattern with an "Attractor" loop: generate, build, validate, score, iterate until satisfaction exceeds a configurable threshold (default 95%). Uses Docker containers for isolated validation and an LLM judge for probabilistic scoring.

### GasTown (Multi-Agent Orchestration)

Steve Yegge's github.com/steveyegge/gastown — Go-based (~189k LOC) framework coordinating 20-30 parallel Claude Code instances. Solves the "50 First Dates" problem (agents with no memory between sessions) using issues stored as JSONL in git (.beads/beads.jsonl), cached in SQLite. Uses Mad Max taxonomy: Mayor (primary coordinator), Polecats (ephemeral workers), The Refinery (merge queue agent).

### Factory.ai (Commercial)

Builds "Droids" for feature development, migrations, code review, testing. Enterprise customers include MongoDB, EY, Zapier. ISO 42001, SOC 2, ISO 27001 certified.

### GitHub Copilot Coding Agent (GA Sep 2025)

Evolved from Copilot Workspace (Apr 2024 - May 2025) which used sub-agents: Plan Agent, Brainstorm Agent, Repair Agent. The Coding Agent boots an ephemeral VM via GitHub Actions, clones repo, analyzes with RAG + code search, implements, tests, creates draft PR. Safety: read-only until creating copilot/ branch, firewall, requester cannot approve own PR, CodeQL scanning on output.

### GitHub Agentic Workflows (Preview Feb 2026)

Defined in Markdown with YAML frontmatter. Run within Actions, support Copilot CLI, Claude Code, or Codex as engines. Read-only by default; writes require "safe outputs" (pre-approved, reviewable operations). Use cases: continuous triage, doc maintenance, test improvement, quality hygiene.

### Sweep AI

github.com/sweepai/sweep — Turns issues into PRs. Understands codebase through dependency graphs + vector search, plans modifications, writes code, runs tests. Supports hosted and self-hosted.

### Devin AI

Operates in a cloud IDE (editor, terminal, browser, planning tools). Plan-execute-verify cycle: "Architectural Brain" breaks down tasks, presents plan for approval, then autonomously executes. Can dispatch sub-tasks to parallel Devin instances.

### OpenHands (Formerly OpenDevin)

Most popular open-source agent platform. V1 SDK uses event-sourced state model with deterministic replay. CodeAct agent architecture connects to environment through IPythonRunCellAction and CmdRunAction. 72% resolution rate on SWE-Bench Verified with Claude Sonnet 4.5.

### Cursor Cloud Agents (Feb 2026)

Autonomous agents on isolated VMs: build, test, record video demos, produce merge-ready PRs. 30% of Cursor's own merged PRs are agent-created.

### CodeRabbit

Webhook-to-Cloud Tasks-to-Cloud Run architecture. Runs changes through 40+ tools (linters, security analyzers). Sandboxing with Cloud Run gen2 + Jailkit + cgroups.

## Key Architecture Patterns

### State Machines

Every implementation uses explicit state machines. Copilot: spec-plan-implement-repair. Devin: plan-execute-verify-iterate. Our design: NEW-CLARIFYING-RESEARCHING-REVIEW_PENDING-APPROVED-COMPLETE with REVISION loop. StrongDM defines development sequences as state machines in Graphviz DOT syntax.

LangGraph provides the most mature framework: interrupt() pauses the graph, saves state via checkpointer, waits until resumed. Maps directly to our pattern of storing session state in PostgreSQL and resuming on webhook events.

### Shadow Repo / Staging Patterns

Most tools use branches within the target repo (Copilot's copilot/ prefix, draft PRs). Our separate-private-repo approach is more thorough than industry standard, closer to the "shadow deployment" pattern from infrastructure. Cloudflare's Agents SDK uses Durable Objects (stateful micro-servers with SQL database per session) for natural isolation.

### Human Approval Gates

Mastra documents four patterns differentiated by whether approval happens before or after tool execution, and whether the entry point is the agent or workflow. Key insight: place approval where the risk sits. Copilot is binary (autonomous then human review). Our four-mode progressive autonomy model is more sophisticated than most commercial tools.

### LLM Output Validation

Industry patterns: output format enforcement, multi-layer defense (input validation + output filtering + privilege minimization), Code-Then-Execute (LLM generates formal program, run in sandbox), automated adversarial testing. Our two-layer safety (structural + LLM reviewer) aligns with best practice.

### Round-Trip Limits

Standard practice sets hard iteration limits (50 iterations on a large codebase costs $50-100+). Our 2-4 round-trip target with escalation at 4 is conservative and appropriate for a research agent.

## Lessons for Our Implementation

### Holdout Scenario Pattern

Most novel idea. Maintain a holdout set of "what good research looks like" that the generating LLM never sees. Use a separate LLM judge to score satisfaction on 0-100 scale. Gives quantitative quality signal beyond binary pass/fail.

### Declarative Workflow Definitions

GitHub Agentic Workflows define workflows in Markdown with YAML frontmatter. If we add more workflow types (research, bug investigation, implementation), declarative definitions would improve maintainability.

### Structured Memory (Beads Pattern)

GasTown's JSONL-in-git with embeddings for retrieval. We're already doing this with JSONB in PostgreSQL; the pattern validates our approach.

### Queue-Based Architecture

CodeRabbit's webhook-to-queue-to-worker decouples handling from execution. Worth adopting when we add implementation agents with code execution.

### Our Advantages

Our shadow repo pattern is genuinely novel — most tools just use branches. Our progressive approval model (approve-everything through budget-gated) with per-repo configuration is ahead of the curve. The key insight from the dark factory literature: jumping from supervised to autonomous requires a different validation architecture (holdout scenarios, probabilistic scoring), not just confidence thresholds.

## Sources

- The Dark Factory Pattern (HackerNoon, Feb 2026)
- Dark Factory Architecture (Infralovers, Feb 2026)
- StrongDM Software Factory (Simon Willison, Feb 2026)
- OctopusGarden (github.com/foundatron/octopusgarden)
- GasTown (github.com/steveyegge/gastown)
- Factory.ai
- GitHub Copilot Coding Agent (GitHub Docs)
- GitHub Agentic Workflows (GitHub Blog, Feb 2026)
- Sweep AI (github.com/sweepai/sweep)
- Devin AI
- OpenHands / OpenDevin (openhands.dev)
- Aider (aider.chat)
- CodeRabbit (Google Cloud Blog)
- Cursor Cloud Agents (Feb 2026)
- HITL Patterns (Mastra Blog)
- LangGraph Interrupts (LangChain Docs)
- OWASP LLM Prompt Injection Prevention
- Multi-Agent Defense Pipeline (arXiv 2509.14285)
