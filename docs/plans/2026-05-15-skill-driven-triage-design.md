# Skill-driven triage design

Status: proposed
Date: 2026-05-15
Relates to: `docs/plans/2026-04-22-research-brief-bot-design.md` (closes the deferred "promotion drafter" question)
Relates to: `docs/plans/2026-04-22-retrieval-engine-plan.md` (consumes the shipped retrieval engine)
Supersedes: nothing (additive)

## Context

The retrieval engine (PR #120, merged 2026-04-24) shipped the data plane the research-brief design called for: `hats.md` loader and parser, regression-window PR diff, Electron release watcher with `blocked`-issue cross-reference, hat-aware soft rerank in vector search, and a `POST /brief-preview` smoke endpoint that returns class, similar past issues, similar docs, regression PRs, and upstream candidates as a single structured JSON. The brief generator and promotion drafter that would have sat on top of that data plane were explicitly deferred. The reason was that the promotion drafter (maintainer-style reply drafting) was being prototyped as a Claude Code skill inside `IsmaelMartinez/teams-for-linux` rather than as a hardcoded Gemini prompt in Go.

That skill, `teams-for-linux-issue-review`, has now matured beyond a promotion drafter. It encodes a full review workflow: sibling-ticket sweep with `gh issue list --search`, code-path reading from `app/`, Microsoft Learn verification for Teams-web claims, log re-fetching with ticket-namespaced paths, four-column Wayland/GPU disambiguation, PR review with inline-comment categorisation, and external-contribution gating. Its section 4 classifies issues into eight categories that overlap heavily with the `hats.md` taxonomy seeded in the research-brief design (`display-session-media`, `tray-notifications`, `upstream-blocked`, etc.). The skill is opinionated, reflects the maintainer's review style, and is invoked manually per issue.

The choice in front of us is whether to wire the skill to the bot's MCP server so it can pull retrieval context as an input alongside everything else it already does, or whether to keep going down the original path of building the brief generator and promotion drafter as Go code that calls Gemini. This plan takes the first option. The rationale is that the skill already exists, already produces calibrated maintainer-voice output, runs in a Claude Code context that can read code and fetch docs (which a hardcoded Gemini prompt cannot), and respects the durable maintainer preference that triage replies stay manually invoked.

## What we are building

Three additive changes. None replaces existing code; the Go-side LLM phases (Phase 2, 4a, synthesis) stay running for the duration of the trial.

### 1. `get_brief_preview` MCP tool

A fifth tool on the bot's existing MCP server (`cmd/mcp`) that wraps `POST /brief-preview`. Takes `repo` and `issue_number` (and optional `hat`), returns the same JSON shape the HTTP endpoint already produces. Allow-list aware via the bot's existing `allowedRepos` map. The skill calls this tool early in its process to pull retrieval context.

### 2. Seed `hats.md` in `IsmaelMartinez/teams-for-linux`

A `.github/hats.md` file derived from the skill's section 4 classification crossed with the eight initial hats the research-brief design suggested. Each hat has the symptom signature, retrieval filter, reasoning posture, and anchor issues. This is a teams-for-linux repo change, not a bot change. The bot reads it at request time via the GitHub Contents API (the loader is already shipped).

### 3. Skill integration step

A new section in `~/.claude/skills/teams-for-linux-issue-review/SKILL.md` instructing the skill to call `mcp__triage-bot__get_brief_preview` between step 1a (sibling-ticket sweep) and step 2 (open the local source). The brief-preview output becomes one input among several. The skill still reads logs, opens code paths, verifies third-party claims, dispatches subagents. The bot's retrieval surfaces three things the skill cannot easily reproduce: vector-search hits over the full corpus (semantic match for older issues with different wording), the Electron release cross-reference (release notes scored against open `blocked` issues), and hat-aware doc retrieval ranked across ADR, troubleshooting, roadmap, and research at once.

## Architecture

The MCP server runs as a stdio JSON-RPC process (`go run ./cmd/mcp`) configured against the deployed Cloud Run URL. The existing four tools (`get_pending_triage`, `get_synthesis_briefing`, `get_report_trends`, `get_health_status`) call aggregate endpoints. The new tool calls the per-issue endpoint and returns its JSON unchanged. No new HTTP routes, no new database queries, no new infrastructure.

The skill stays a manual invocation. The user picks an issue, fires the skill, the skill calls the MCP tool, retrieves the brief-preview JSON, weaves it into its existing process, drafts the reply. The maintainer approves before posting. This respects the durable feedback memory that the skill should not be auto-fired by hooks or cron.

## File structure

Create:
- `internal/mcp/tools/brief_preview.go` — tool definition and handler
- `internal/mcp/tools/brief_preview_test.go` — handler unit tests

Modify:
- `internal/mcp/tools/tools.go` — register the new tool alongside the four existing ones
- `cmd/mcp/main.go` — wire the tool into the tool registry (the existing pattern from `pending_triage`, etc.)

Outside this repo:
- `IsmaelMartinez/teams-for-linux:.github/hats.md` — new file with 8 hats drawn from skill section 4 plus research-brief seed list
- `~/.claude/skills/teams-for-linux-issue-review/SKILL.md` — new "Step 1c: pull bot retrieval context" section between 1a and 2

## Tasks

### Task 1: Add `get_brief_preview` MCP tool

Files:
- Create: `internal/mcp/tools/brief_preview.go`
- Create: `internal/mcp/tools/brief_preview_test.go`
- Modify: `internal/mcp/tools/tools.go` (register)
- Modify: `cmd/mcp/main.go` (wire)

- [ ] Write failing test in `brief_preview_test.go` covering: required args validation (repo, issue_number both required), pass-through of optional `hat`, HTTP body shaping, error mapping (404 → tool error, 500 → tool error), happy-path JSON decode.
- [ ] Run `go test ./internal/mcp/tools/ -run TestBriefPreview` and confirm failure.
- [ ] Implement `BriefPreview()` returning `Tool{Def, Handler}` in `brief_preview.go`. Mirror the `pending_triage.go` shape: tool def with name `get_brief_preview`, description, JSON-schema input (`repo` string required, `issue_number` integer required, `hat` string optional), handler that POSTs to `{baseURL}/brief-preview` with `{repo, issue_number, hat?}` body and returns the parsed JSON as the tool result.
- [ ] Add `BriefPreview` to the tools registry in `tools.go` (parallel to `HealthStatus`, `PendingTriage`, `ReportTrends`, `SynthesisBriefing`).
- [ ] Wire registration in `cmd/mcp/main.go`.
- [ ] `go test ./...` → all green.
- [ ] `golangci-lint run ./...` → clean.

### Task 2: Seed `hats.md` in teams-for-linux

Files:
- Create: `IsmaelMartinez/teams-for-linux:.github/hats.md`

The bot's hats parser (`internal/hats/parser.go`) accepts a markdown file with one `## hat-name` per hat, body containing freeform prose plus optional bracketed metadata keys (`retrieval_boost: keyword1,keyword2`, `posture: workaround-menu | single-hypothesis | causal-narrative | demand-gating | config-check`, `anchor_issues: 2169,2138`).

The eight initial hats, drawn from the research-brief design's seed list and the skill's section 4 categories:

- [ ] `display-session-media` — screen-share, camera, screen-capture, window-picker, display-server interactions. Posture: causal-narrative when one mechanism dominates, workaround-menu when display server + GPU vendor + compositor + Electron version split the diagnosis. Anchors: #2169 (Electron 39.8.2 VideoFrame fix), #2138, #2529 (multi-account session bound to root only).
- [ ] `internal-regression-network` — auth, network, SSO, certificate, MITM proxy regressions where a TFL release coincides with the symptom appearing. Posture: causal-narrative. Anchors: #2293.
- [ ] `tray-notifications` — tray icon, notification rendering, notification sound, badge, MQTT status. Posture: single-hypothesis when one code point is clearly responsible, workaround-menu when configuration permutations matter. Anchors: #2239, #2248, #2095.
- [ ] `upstream-blocked` — Electron, Chromium, Microsoft Teams web, or system-portal limitations with no current workaround. Posture: blocked-on-upstream tag, document the limitation. Anchors: #2335, #2137.
- [ ] `packaging` — snap, flatpak, AUR, deb, rpm-specific failures that would not reproduce on other packaging. Posture: config-check. Anchors: #2239.
- [ ] `configuration-cli` — config-file or command-line option that toggles the symptom; user does not know the right setting. Posture: config-check. Anchors: #2143, #2205.
- [ ] `enhancement-demand-gating` — feature request from a non-contributor. Posture: demand-gating-needed. Surface the four extension patterns from the skill's section 4 (main-process surface, webRequest interception, React-store hijack, floating UI plus synthetic events). Anchor: #2107.
- [ ] `auth-network-edge` — SSO, FIDO2, federated auth, certificate, network edge that needs end-to-end diagnostic command paste. Posture: workaround-menu. Anchors: #2326, #2364.

Each hat is one short paragraph, not bullets. The file stays under 2000 tokens (the loader's soft size guard) so the entire taxonomy fits in the brief generator's system prompt once that ships. The maintainer commits this directly to teams-for-linux; the bot's `config.Cache` picks it up via the Contents API on next request (5-minute TTL).

### Task 3: Skill integration step

File:
- Modify: `~/.claude/skills/teams-for-linux-issue-review/SKILL.md`

Insert a new subsection between current sections 1a and 2:

```
### 1c. Pull bot retrieval context

After the sibling-ticket sweep and before opening the local source, call the
triage bot via MCP to pull its retrieval bundle for the issue:

`mcp__triage-bot__get_brief_preview` with `repo=IsmaelMartinez/teams-for-linux`
and `issue_number=NNNN`.

The response contains:
- `class` — the hat the bot picked from `.github/hats.md` (or `other`)
- `similar_past_issues` — top-5 semantic neighbours from the full corpus
- `docs` — top-5 doc hits across ADR, roadmap, research, troubleshooting
- `regression_prs` — merged PRs between reporter's working version and latest
  release, only if the reporter named a working version
- `upstream_candidates` — open `blocked` issues semantically close to this one,
  used to flag Electron-release cross-references

Read the bundle but do not trust the rankings blindly. The vector search has
the same calibration failures simili-bot has shown (semantic neighbours can
be older issues that share keywords but not root cause). Treat it as input,
not output. Apply the four-column check from section 3e before citing any
neighbour; verify any cited doc before quoting; the regression PR list is
keyword-filtered and can include unrelated merges, so read the diffs before
citing.

The bot's retrieval adds three things `gh issue list --search` does not:
semantic neighbours older than the keyword window, the doc index across all
four classes, and the upstream-release cross-reference. Everything else you
do yourself.
```

This addition is small (one section). It changes the skill's behaviour from "gh-only" to "gh plus bot retrieval, both as inputs," without changing the skill's calibration discipline, anti-patterns, or voice. The skill remains the brain.

### Task 4: Quality evaluation

Files:
- Create: `docs/plans/2026-05-15-skill-driven-triage-eval.md` (or append to this plan)

Run the integrated skill against the most recent 10 open issues in teams-for-linux. For each, post the skill's draft to the shadow repo as a `[Sim] [class] #NNNN: <title>` issue cross-linked to the existing `[Triage]` produced by the current Go pipeline. The maintainer reads both and records:

- which output surfaced the more useful diagnosis or workaround
- which output contained any factual errors or fabricated references
- which output's "next steps" were the ones the maintainer would actually take
- a one-line note per issue capturing the qualitative difference

After 10 issues, decide:

- if the integrated skill outperforms in 7 or more cases, expand the trial to 30 more issues and prepare a Phase 5 to retire Phase 2/4a/synthesis from the public bot comment path
- if the bot's Phase 2/4a/synthesis output is competitive, leave both running for another month and re-evaluate
- if the skill underperforms, document why in the open-questions section below and either tune the skill or rework retrieval

The eval is structured for honesty: the simulated outputs go to shadow (low blast radius), the comparison is recorded against existing `[Triage]` outputs already posted there, and no public-comment policy changes until the decision is taken.

## Testing

The MCP tool has unit tests at the same coverage level as the four existing tools (input validation, HTTP error mapping, JSON decode). End-to-end testing is the quality eval in Task 4.

`hats.md` is parsed by `internal/hats/parser.go`, which already has table-driven tests. Adding a new hats file in teams-for-linux is not a code change here, so no Go test additions are needed. The existing parser tests cover the format.

The skill change is a documentation change; there is no automated test. Validation is qualitative through the Task 4 eval.

## Migration

Nothing to migrate. The Go-side phases stay running. The MCP tool is additive. `hats.md` is opt-in via `butler.json` `research_brief.enabled` (currently defaults false). The skill change is local to `~/.claude/skills/`.

If the eval supports retiring Phase 2/4a/synthesis from the public bot path, that becomes a follow-up plan with its own migration: the webhook handler would stop calling those phases, the public bot comment would shrink to Phase 1 only (missing-info nudges), and the rest of the data would live in shadow for skill-driven review. That follow-up is out of scope here.

## Security and safety

The MCP tool inherits the bot's existing allow-list and authentication. The `/brief-preview` endpoint already requires repo membership in `allowedRepos`. No new authentication surface.

The skill output is drafted in a Claude Code session, posted by the maintainer (or by an authenticated tool call invoking `gh issue comment` after the maintainer approves). Existing safety layers in the bot (`internal/safety/structural.go`, `internal/safety/llm_validator.go`) do not apply to skill output because the skill bypasses the bot's comment-builder path. The skill's own discipline (anti-patterns section) is the safety boundary. Posting goes through the maintainer's `gh` token, not the bot's GitHub App.

The `[Sim]` shadow issues during the eval are clearly marked and cross-linked to existing `[Triage]` issues, so the maintainer can read both without confusion. The shadow repo is private to the maintainer and not visible to users.

## Open questions

How does the skill handle the case where `/brief-preview` returns empty similar-issues or empty docs? It needs to keep working (the brief endpoint already returns empty slices rather than 500). The skill's section 1c needs a line saying "absence is data": if the bot finds nothing semantically close, that is itself a signal that the issue is novel.

How do we handle the simili-bot trial overlap? Both simili-bot's similar-thread list and the bot's `similar_past_issues` claim to surface neighbours. The skill currently warns against trusting simili-bot's confidence numbers. The skill needs the same warning about the bot's vector hits. The simili-bot trial closes adopt/extend/drop after about 10 more issues (per the trial decision doc); the answer can wait until then.

Should the skill's section 1c also pull from `mcp__triage-bot__get_synthesis_briefing` for context on weekly clusters and drift? Maybe, but that pulls cross-issue context that may not be relevant to one ticket. Hold off until the brief-preview integration has been validated.

What happens to the existing `internal/agent` flow (research synthesis on enhancement issues, lgtm gate, shadow promotion)? That path is orthogonal: it triggers on enhancement issues and produces `[Research]` shadow output. The skill-driven triage covers all issue types. The two can coexist; if the eval supports retiring Phase 2/4a/synthesis, the `internal/agent` research path can be revisited separately.
