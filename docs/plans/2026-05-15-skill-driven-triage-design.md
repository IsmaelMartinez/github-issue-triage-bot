# Skill-driven triage design

Status: implemented (Tasks 1-3); Task 4 pilot complete, retirement of Go-side phases deferred pending real-use evidence
Date: 2026-05-15 (updated 2026-05-16)
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

### Task 1: Add `get_brief_preview` MCP tool — DONE

Shipped in PR #144 (merged 2026-05-16, squash commit `4fce834`).

Files shipped:
- `internal/mcp/tools/brief_preview.go` — tool definition and handler
- `internal/mcp/tools/brief_preview_test.go` — table-driven unit tests covering def shape, missing-args, happy-path body shaping, optional hat field, auth header, HTTP error mapping, bad-JSON args
- `internal/mcp/tools/tools.go` — added `postJSONWithBody` helper (the existing helpers did not support POST with a JSON body); `doRequest` signature extended to accept an optional body
- `cmd/mcp/main.go` — registered the new tool; tool count `tools` log field bumped 4 → 5
- `docs/adr/013-mcp-server.md` — amended to mention the fifth tool

The tool wraps `POST /brief-preview`. Signature: `get_brief_preview(repo, issue_number, hat?)`. Response is the unchanged JSON from the HTTP endpoint: `{class, similar_past_issues, docs, regression_prs, upstream_candidates}`. Allow-list enforcement happens server-side via the existing `allowedRepos` check.

### Task 2: Seed `hats.md` in teams-for-linux — DONE

Shipped in IsmaelMartinez/teams-for-linux PR #2548 (merged 2026-05-15, squash commit on main). Three gemini-code-assist medium-priority review comments were addressed before merge: `internal-regression-network` Phase 1 asks now user-facing only (maintainer-side checks moved to "When to pick"); `packaging` "When to pick" tightened to discriminate from `tray-notifications`; `auth-network-edge` Chromium-shared-plumbing interpretation caveat moved out of Phase 1 asks and into "When to pick".

Nine hats live at `.github/hats.md` in teams-for-linux (eight from the design plus `other` as fallback). Parser verification (`internal/hats/parser.go`) returned correct postures, retrieval-boost keywords, and anchor issue numbers for every hat.

Effect: `POST /brief-preview` now returns `class: <hat-name>` when the caller passes a `hat` parameter and applies `ApplyHatBoost` to the doc retrieval. The empirical effect was confirmed during the Task 4 evaluation (see below): on three of seven hatted simulations the hat-aware boost surfaced load-bearing ADRs that unboosted retrieval would have missed.

### Task 3: Skill integration step — DONE

Applied 2026-05-16 directly to `~/.claude/skills/teams-for-linux-issue-review/SKILL.md`. Section 1c sits between the sibling-ticket sweep (1a) and opening the local source (2), instructing the skill to call `get_brief_preview` with the appropriate hat from `.github/hats.md`, treat the response as input not output, apply the four-column check before citing any neighbour, and note the spurious `[802]` upstream candidate to ignore for now.

**Distribution decision (2026-05-16):** the skill stays local to the maintainer's Claude config and is *not* committed to either this repo or teams-for-linux. The skill encodes the maintainer's personal review style and is also used to review external PRs, so committing it to teams-for-linux risks contributors interpreting it as the canonical project policy rather than a personal workflow. A generic template could be extracted into this repo if and when a second consumer of the bot wants the same pattern, but that is deferred until concrete second-consumer feedback exists.

### Task 4: Quality evaluation — PILOT COMPLETE, decision deferred

Ran on 2026-05-15 against the last ten open teams-for-linux issues. Drafts posted as `[Sim] #NNNN: <title>` issues in `IsmaelMartinez/teams-for-linux-shadow`, cross-linked to the existing `[Triage]` produced by the Go pipeline.

Simulation set:

| Source | [Sim] | [Triage] | Hat | Hat-aware? |
|---|---|---|---|---|
| #2541 | shadow#97 | shadow#95 | other | no (pilot, hats.md not yet merged) |
| #2534 | shadow#99 | shadow#91 | other | no (pilot) |
| #2530 | shadow#98 | shadow#88 | other | no (pilot) |
| #2536 | shadow#100 | shadow#93 | auth-network-edge | yes |
| #2535 | shadow#101 | shadow#92 | upstream-blocked | yes |
| #2529 | shadow#102 | shadow#87 | display-session-media | yes |
| #2524 | shadow#103 | shadow#85 | enhancement-demand-gating | yes |
| #2518 | shadow#104 | shadow#82 | other (taxonomy gap) | n/a |
| #2512 | shadow#105 | shadow#81 | enhancement-demand-gating | yes |
| #2508 | shadow#106 | shadow#80 | configuration-cli | yes |

Findings:

The skill drafts outperformed `[Triage]` on every case. On the three cases where `[Triage]` had any opinion at all (`#95`, `#91`, `#92`), the skill produced a sharper diagnosis grounded in the local source rather than the bot's keyword-adjacent neighbour list. On the seven cases where `[Triage]` was a verbatim mirror of the issue body with no diagnostic content, the skill drafts added the missing analysis. The decision threshold in the original criterion ("7 or more cases") was met (10 of 10).

The strongest single value-add from the skill was reading local repo state to detect in-flight PRs that reference the issue: #2530's draft cited PR #2540 (the `.catch()` fix), #2524's draft confirmed PR #2543 had already shipped and closed the loop, #2529's draft cited PR #2533 on develop, #2518's draft credited PR #2520's `--disable-quic` approach, #2536's draft cited PR #2250's regex update from two months prior. The Go pipeline cannot produce this kind of context because it does not check open or merged PRs against the issue.

Hat-aware retrieval was material on three of seven hatted simulations: `display-session-media` for #2529 surfaced ADR 010 and ADR 008 (load-bearing); `configuration-cli` for #2508 surfaced the three Electron upstream PRs justifying the ozone-platform default change; `auth-network-edge` for #2536 surfaced the closest precedent (#1059). On three more (`upstream-blocked` for #2535, `enhancement-demand-gating` for #2524 and #2512) the hat was directionally right but the neighbour list still mostly keyword-noisy. On one (`other` for #2518) the hat was missing from the taxonomy and the unboosted retrieval surfaced a structurally similar but mechanism-wrong upstream Electron PR pair (#50559/#50670) that misled the bot's already-posted public reply.

Decision: not retiring the Go-side phases yet. The simulation evidence is sufficient by the original criterion, but several wins (in-flight PR detection, local code reading) come from the skill operating in a Claude Code session rather than from the bot's retrieval, and the durable feedback preference is to keep the skill manually invoked. Until the skill is in routine real use rather than a one-shot simulation batch, retiring Phase 2/4a/synthesis from the public bot comment path is premature. Re-evaluate after thirty real-use invocations.

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

## Follow-ups identified during the pilot

Three taxonomy and retrieval observations from the simulation that became concrete follow-ups:

A `downloads` hat is worth adding. Two of the last ten issues (#2512 feature request for progress indicators, #2518 concurrent download stall) had no clean fit in the current taxonomy and defaulted to `other` or `enhancement-demand-gating`. The latter was a stretch in both cases. A `downloads` hat with keyword boost on `download`, `will-download`, `DownloadItem`, `shell.openExternal`, `setDisplayMediaRequestHandler` would route similar future cases properly. Defer until a third download-area issue accumulates.

A `tracking-validation` hat is worth adding. #2508 (the ozone-platform default validation tracking issue) is a cohort-roll-up shape, not a diagnostic shape. `configuration-cli` was the closest available match but its Phase 1 asks are wrong for a tracking issue. The new hat would have a `tracking-validation` posture that signals "do not run Phase 1 missing-info checks on this issue, summarise the cohort instead." Worth adding when a second tracking issue surfaces; one example is not yet enough to anchor a hat.

The `upstream_candidates: [802]` constant returned by `/brief-preview` for every query is a bug, not a feature. The FIDO2 thread #802 is being returned regardless of the issue, polluting the hat-aware bundle with a spurious upstream cross-reference. Root cause is most likely in `FindSimilarBlockedIssues` (the only `blocked` issue with a recent embedding) but worth a focused look at the query and the threshold. File as a separate bug.

## Open questions

~~How does the skill handle empty similar-issues or empty docs?~~ Answered in the shipped section 1c edit: "Absence is data too: if the bot finds nothing semantically close, that itself is evidence the issue is novel."

How do we handle the simili-bot trial overlap? Still open. Both simili-bot's similar-thread list and the bot's `similar_past_issues` surface neighbours. The skill's existing anti-pattern warning about simili-bot's duplicate confidence numbers now also applies to the bot's vector hits, as observed across all ten simulations. The simili-bot trial closes adopt/extend/drop after about ten more issues per `docs/decisions/007-simili-bot-trial.md`; revisit then.

Should the skill's section 1c also pull from `mcp__triage-bot__get_synthesis_briefing` for context on weekly clusters and drift? Hold off. The brief-preview integration is the validated path; pulling weekly cross-issue context for a single-ticket review would add noise more often than signal.

What happens to the existing `internal/agent` flow (research synthesis on enhancement issues, lgtm gate, shadow promotion)? Still orthogonal. It triggers on enhancement issues and produces `[Research]` shadow output. The skill-driven triage covers all issue types. Both can coexist until the Phase 2/4a/synthesis retirement decision is taken; at that point the `internal/agent` research path is revisited separately.

When should retirement of Go-side phases (Phase 2, 4a, synthesis) be reconsidered? After thirty real-use skill invocations on public teams-for-linux issues, not pilot simulations. The criterion: if the skill draft was the one the maintainer actually posted (or used as the base for the posted comment) in twenty-five or more of those thirty, retire those phases and shrink the public bot comment to Phase 1 only.
