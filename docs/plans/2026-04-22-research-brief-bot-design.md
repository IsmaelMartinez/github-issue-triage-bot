# Research-brief bot design

Status: proposed
Date: 2026-04-22
Partially supersedes `docs/plans/2026-03-18-repository-strategist-design.md` (Phase 2, 4a, synthesis).
Retains `docs/plans/2026-03-15-lean-bot-pivot-design.md` (Phase 1 kept as-is; later phases replaced).

## Context and motivation

The current bot describes itself as doc-grounded research but the execution leaks into problem-solving. Phase 2's Gemini prompt asks the LLM for "what the user should try", and real bot comments offer diagnoses such as "this sounds like our token refresh implementation — ADR-003". That framing tries to be helpful but is wrong-shaped. For ambiguous issues the bot commits to a single hypothesis with weak evidence. For regressions it misses the recent-PR diff that would point at the actual cause. For classes of issue that historically need a workaround menu it still produces one-hypothesis answers.

Separately, simili-bot is being trialed for duplicate detection and GitHub's native AI handles labels, so this bot does not need to keep doing either. The remaining job is context finding: given an issue, surface everything a maintainer needs to triage fast and well, without pretending to know the answer. The maintainer stays the decision-maker.

This document specifies a rethink that keeps the parts of the current bot working well (Phase 1 missing-info prompting, the existing shadow-repo and agent-session plumbing), replaces the parts that leak into solving (Phase 2, 4a, and synthesis), and adds three engine capabilities the current bot lacks: upstream changelog watching, regression-window PR diff, and heterogeneity tracking.

## What we are building

A research-brief generator that runs on every issue event, produces a structured brief shaped by a per-repo taxonomy of issue classes (called hats), and posts the brief to the repo's shadow. The maintainer reviews in shadow and either drops the brief or promotes it. On promote, the bot drafts a publishable comment for the maintainer to post (with optional edits) to the original issue.

The design is adaptive. Each repo has a `hats.md` file listing its classes of issues and per-class reasoning posture. A setup skill runs a questionnaire with the maintainer that drafts `hats.md` for a new repo, then the maintainer edits it like an ADR.

The brief itself follows a fixed schema with hat-shaped variations. Confidence stays to `high` and `medium`; `low` hypotheses drop entirely rather than padding the output. Workarounds lead because when someone is blocked, something that works today beats a correct story about who is at fault.

## Architecture

Four components, each with one clear purpose.

### Brief generator

Webhook-triggered on issue events (`opened`, `edited`, `reopened`). Given an issue it embeds the body, retrieves context from the vector store, runs the regression-window diff if the reporter named a working version, checks the upstream changelog index for plausible matches, and makes a single LLM call with `hats.md` as system prompt. The LLM picks a hat, fills the brief schema, and obeys the hat's reasoning posture (workaround menu, single hypothesis, causal narrative, demand-gating, config-check). Output lands in the shadow repo.

The single-LLM-call design keeps per-issue cost predictable and leaves the hat structure transparent. The maintainer can read `hats.md` and know what the bot was told. If no hat fits (unusual issue shape), the LLM selects `other` and emits a generic brief without hat-specific posture — a signal to the maintainer that `hats.md` may need a new entry.

### Promotion drafter

When the maintainer signals `lgtm` or `promote` on a shadow-repo brief, a second LLM call drafts a publishable comment shaped from the brief. The result is research-flavoured: no hypothesis-as-fix, workarounds positioned as options not instructions, causal narrative drafted for maintainer review. The bot posts directly on promote, or waits for maintainer edits via the existing shadow-to-public flow.

### Changelog watcher

A daily cron job fetches upstream release notes from configured dependencies (Electron by default for Electron apps, extensible per repo). For each release the watcher embeds the release notes, searches the vector store for open `blocked` issues whose symptoms plausibly match, and posts a note to the shadow repo listing candidate matches with a confidence score. The maintainer decides whether to comment on the `blocked` issues. A persistent `cross_reference_index` keyed on (consumer_repo, release_tag, issue_number) records each processed (issue, release) pair so the watcher does not re-notify on the same match when a re-run cross-references older releases against newly-labelled `blocked` issues.

This replaces the ambient practice of the maintainer scanning Electron release notes by hand, which is the signal that currently closes issues like #2169 (Electron 39.8.2 VideoFrame fix) and #2335 (Electron 41 Wayland input work).

### Heterogeneity tracker

A per-repo record of past workaround-to-default promotions. For each entry it records when the workaround was promoted (which PR, which release) and tracks whether issues tagged with the affected symptom surfaced afterwards. When a new brief would recommend "promote workaround X to default", the tracker surfaces the history of past promotions and any downstream breakage reports. The reasoning behind this capability: defaults across heterogeneous users are risky, and past decisions should inform new ones rather than being rediscovered.

## Brief schema

The brief is Markdown with a fixed structure. Every brief has these sections; content is hat-dependent.

Class — one-line label, drawn from `hats.md`.

Regression window — if the reporter names a working version, the span of versions between working and reported. Empty otherwise. The bot asks for the working version in the diagnostic asks when missing.

Recent changes in regression window — populated only when a regression window exists. Output of `git log <working>..<reported>` keyword-filtered to the symptom domain (keywords extracted by the LLM from the issue body). Each PR gets a one-line relevance note. The list is capped at the 50 most recent merged PRs in the window; if the window spans more than 50 PRs the brief notes the truncation so the maintainer can narrow the working version.

Similar past issues — top N issues from vector search, filtered to `high` and `medium` relevance only. Each entry: issue number, one-line summary, how it was resolved, and the relevance tag. The LLM decides `high` or `medium` based on symptom overlap; `low` is dropped so the brief stays clean and readers can trust the list.

Project docs — relevant ADRs, roadmap items, troubleshooting entries. Omitted entirely if fewer than 2 high-or-medium-relevance hits so the brief does not pad with thin results.

Upstream signals — relevant Electron/Chromium/Microsoft or other dependency notes, populated from the changelog-watcher index and direct retrieval. Includes an explicit "no matching tracker found" when retrieval came up empty, because absence is informative.

Phase 1 gaps — what template sections are empty or weak. Mirrors the current bot's Phase 1 output. Phase 1 also continues to post publicly as today (see Migration), so this section in the brief is a summary of what has already been asked in the issue, so the maintainer sees the complete picture in one place.

Workarounds — first content section the maintainer sees. Ordered cheapest first. For hats with workaround-menu posture this is the main content and the hypotheses are secondary. Always present, always before hypotheses.

Hypotheses — `high` and `medium` confidence only, maximum three. Each includes the fork that would flip the ranking ("if this reproduces under native Wayland, demote #1"). The causal narrative, when the hat supports drafting one, is embedded under the lead hypothesis as prose.

What would flip the ranking — an explicit decision-tree hint: what signal would change the posture.

Triage posture tag — one of `upstream-likely`, `internal-regression`, `config-dependent`, `ambiguous-workaround-menu`, `blocked-on-upstream`, `demand-gating-needed`. Used by the maintainer to orient at a glance and by the promotion drafter to shape the public comment.

## The `hats.md` format

Markdown file at repo root or under `.github/`. One H2 per hat. Each hat defines: the symptom signature the LLM should match, the retrieval filter (which past-issue labels, which doc classes, which upstream dependencies to weight as soft reranking rather than hard filter), the reasoning posture, and one or two example issue numbers that anchor the hat with concrete past cases.

The file is git-tracked and edited by the maintainer like any doc. The LLM loads the entire file as system prompt, so keep it under a few thousand tokens — rule of thumb is eight to twelve hats, each under a paragraph. A loader-side size check emits a warning when the file approaches the LLM's context window; at that point the taxonomy should be split by domain (separate files per repo area) rather than retrieved via RAG, because keeping the whole taxonomy visible to the LLM at classification time is load-bearing for hat-selection quality.

Initial seed for teams-for-linux: `display-session-media`, `internal-regression-network`, `tray-notifications`, `upstream-blocked`, `packaging`, `configuration-cli`, `enhancement-demand-gating`, `auth-network-edge`. Each has example issues drawn from past triage: #2169 and #2138 anchor `display-session-media`; #2293 anchors `internal-regression-network`; #2239, #2248, #2095 anchor `tray-notifications`; #2335 and #2137 anchor `upstream-blocked`; #2239 also anchors `packaging`; #2143 and #2205 anchor `configuration-cli`; #2107 anchors `enhancement-demand-gating`; #2326 and #2364 anchor `auth-network-edge`.

## Setup skill

New-repo onboarding runs a Claude Code skill that interviews the maintainer and generates an initial `hats.md`, a `butler.json` with research-brief-bot config, and a list of upstream dependencies to watch. The questionnaire covers primary platform and runtime (electron, node, python, go, etc.), upstream frameworks to track, packaging variants the project ships, common symptom classes the maintainer has seen, and per-class reasoning posture.

The skill is packaged alongside this repo so it can be invoked from any other repo that wants to onboard the bot. The skill generates drafts, not finals — the maintainer edits before committing. Optionally the skill picks three recently-closed issues, drafts sample briefs against them, and shows the maintainer so they can see the bot's output before committing to the config.

## Pipeline

A webhook fires. The handler parses the event, checks the repo has a `butler.json` with research-brief-bot enabled, and enqueues a brief job.

The job runs through: embed the issue body; retrieve similar past issues (broad first pass, then hat-filtered soft rerank once hat is picked); retrieve relevant docs; if the reporter names a working version, run the regression-window diff; check the changelog-watcher index for plausible upstream matches; run Phase 1 (unchanged); make a single LLM call with `hats.md` as system prompt, the issue body as user message, and all retrieved context appended; parse the LLM output into the brief schema and validate; post to the shadow repo via existing mirroring plumbing.

Phase 1 continues to run as a separate, deterministic step because it is low-LLM and metric-validated. Its output is appended to the brief's Phase 1 gaps section.

On a shadow-repo `lgtm` signal (existing agent-session pattern), the promotion drafter runs, produces a publishable comment, and the bot posts it to the original issue — matching the existing shadow-to-public promotion pattern.

## Migration from current bot

Keep: Phase 1 (missing info), shadow repo plumbing, agent-session `lgtm` flow, `butler.json` config system, webhook handler core, vector store, event journal, cross-reference index, existing safety validators.

Replace: Phase 2 (doc-grounded troubleshooting), Phase 4a (enhancement context), synthesis step, comment builder in its current "consolidate phase outputs" form (Phase 1 output still needs formatting, but the diagnostic composition logic goes away).

Add: `hats.md` parser and loader; regression-window diff runner that wraps `git log`; changelog watcher cron and index; heterogeneity tracker schema and queries; promotion drafter; setup skill.

Remove from the bot's public surface: any auto-commenting of diagnostic or suggestive content. Phase 1 missing-info nudges stay public because they are narrow and reaction-positive. Everything else — hypotheses, workarounds, causal narratives, doc suggestions, similar-issue lists — lives in the shadow repo only until the maintainer promotes. The brief in shadow includes a Phase 1 gaps summary (see Schema) purely for the maintainer's single-pane view.

## Non-goals

Duplicate detection is not in scope (delegated to simili-bot trial). Label inference is not in scope (delegated to GitHub native AI). Fine-tuning a custom model is a possible future direction but not necessary now — retrieval plus `hats.md` as system prompt is the reasoning injection mechanism, and we should see how far that takes us before investing in fine-tuning. A web UI is not in scope; the shadow repo is the maintainer's review surface.

## Testing

Phase 1 retains its current table-driven unit tests. No changes.

The brief generator is tested via golden-brief fixtures: a set of past issues plus expected brief outputs. LLM output is non-deterministic so the tests assert structural properties — required sections present, confidence labels valid, at most three hypotheses, hat selected from `hats.md` — and keyword presence in the right sections rather than exact match.

The regression-window diff runner has deterministic unit tests against a fixture git repo.

The changelog watcher has integration tests against cached Electron release notes.

The promotion drafter is tested by running it over fixture briefs and asserting schema plus absence of banned patterns (no "you should try", no "fix this by", no direct imperatives outside the workarounds section). A secondary LLM safety-review pass (the existing `internal/safety/llm_validator.go`) runs after schema validation and before posting; it catches prompt-injection, off-topic drift, and tone issues that pattern-matching cannot. Unit tests cover the LLM validator against fixture outputs with known problematic cases.

## Security and safety

The shadow-repo destination reduces blast radius: the bot cannot say something wrong in public until the maintainer promotes. On promote, existing safety layers apply — structural validator for length, URLs, mentions, and control characters; LLM reviewer for relevance, tone, and prompt-injection detection. The promotion drafter output passes through both before posting.

The changelog watcher only reads upstream release notes; it does not execute or install anything. Its output lands in shadow only.

The regression-window diff uses the GitHub REST API (`/repos/{owner}/{repo}/git/ref/tags/{tag}` → `/repos/{owner}/{repo}/git/commits/{sha}` → `/search/issues?q=repo:X is:pr is:merged merged:A..B`), not shell `git log`. Tag strings come from GitHub's releases API or are extracted from the reporter's issue body via a strict numeric-semver regex (`[0-9]+\.[0-9]+(?:\.[0-9]+)?`) before being used as URL path components, so there is no shell-injection surface.

The setup skill generates files as drafts for the maintainer to edit before committing. The skill does not commit or push directly.

## Open questions

Whether the promotion drafter should post directly on `lgtm` or always open a PR to the original issue's comment stream. Current inclination: post directly but log the action, and escalate to PR-based flow later if any misposts occur.

Whether the heterogeneity tracker should auto-downgrade hypotheses that match a past broken-default pattern, or just surface the history and let the LLM decide. Current inclination: surface only, no auto-downgrade — transparency over implicit rules.

Whether hat retrieval filters should be a hard filter (only search past issues tagged X) or a soft rerank boost. Current inclination: soft rerank, because rigid filters miss cross-hat matches that turn out to be the real neighbour.

Where to run the changelog watcher cron: the existing `dashboard.yml` workflow has capacity, or a new `changelog-watcher.yml`. Current inclination: new workflow, because daily cadence is different from the dashboard's daily aggregate and the outputs go to different places.
