# Simili-bot Duplicate Detection Trial

Date: 2026-04-16
Status: Not started
Owner: IsmaelMartinez/teams-for-linux

## Summary

Trial [simili-bot](https://github.com/similigh/simili-bot) on `IsmaelMartinez/teams-for-linux` to fill the duplicate detection gap left when Phase 3 was removed in the 2026-03-15 lean pivot. Simili-bot runs as a GitHub Action, uses Gemini embeddings + Qdrant for semantic similarity, and in `similarity-only` mode only posts related-issue suggestions without taking labelling or closing actions. Runs alongside the triage bot — no integration, no shared state.

The goal is a bounded, reversible evaluation: can a commodity external tool cover duplicate detection well enough that we never need to rebuild Phase 3?

## Why Now

Roadmap `docs/plans/2026-03-04-roadmap.md:322` lists this as step 38, the highest-priority tractable next action. Preconditions are met: v0.1.0 is out, triage bot is stable on teams-for-linux, and "What We're Not Doing" (`roadmap.md:335`) explicitly names simili-bot as the intended replacement for Phase 3.

## Success Criteria

After 30 days of running on teams-for-linux:

- **Precision:** >60% of simili-bot's suggested duplicates are acknowledged by the maintainer as genuine duplicates or clear relatives (measured by maintainer reactions or follow-up labelling).
- **Coverage:** at least 50% of the duplicates the maintainer would have flagged manually in the trial window are surfaced by simili-bot.
- **Noise floor:** <20% of new issues receive a simili-bot comment that the maintainer judges as unhelpful noise.
- **Cost:** stays within the Gemini free tier when combined with the triage bot's usage; measured from the existing LLM budget tracker and simili-bot's own logs.
- **No regressions:** triage bot's Phase 1 fill rate and dashboard metrics do not degrade (i.e. users are not confused by two bots commenting).

Outcome decisions:
- All four hit → adopt permanently, update roadmap "What We're Not Doing" to cite the result, note in README.
- Precision or noise fails → tune threshold / mode, extend trial 14 days, re-measure.
- Coverage fails but precision holds → keep as a supplement, document the gap.
- Cost fails → drop, document why, revisit if Gemini pricing changes.

## Scope

### In scope

- Install simili-bot on `IsmaelMartinez/teams-for-linux` in `similarity-only` mode (no auto-close, no auto-label).
- Seed simili-bot's Qdrant index from existing closed issues (same corpus the triage bot has embedded).
- Let both bots comment on new issues for 30 days.
- Weekly lightweight review: skim simili-bot's last 7 days of comments, classify each as useful / neutral / noise, log in a tracking issue on this repo.
- Final write-up as a decision record (`docs/decisions/007-simili-bot-trial.md`) capturing outcome, metrics, and recommendation.

### Out of scope

- No MCP integration. Simili-bot is a peer tool, not a dependency.
- No code changes to this repo's triage pipeline.
- No attempt to consume simili-bot findings inside the synthesis engine.
- No multi-repo rollout until teams-for-linux trial concludes.

## Implementation Steps

### Step 1: Install simili-bot on teams-for-linux

- Read simili-bot's README for current install instructions and required secrets.
- Add the GitHub Action workflow to `IsmaelMartinez/teams-for-linux`.
- Configure in `similarity-only` mode with a conservative threshold (start at the tool's recommended default).
- Add `GEMINI_API_KEY` and any Qdrant credentials to teams-for-linux repo secrets (separate key from the triage bot to keep quota attribution clean).

### Step 2: Seed the index

- Use simili-bot's bulk-seed path (or a one-shot backfill Action run) to embed the closed-issue corpus.
- Confirm index size roughly matches the 1,362 issues the triage bot has embedded.

### Step 3: Open a tracking issue

- Create a tracking issue on this repo (`github-issue-triage-bot`) titled `Simili-bot trial — teams-for-linux (2026-04-16 → 2026-05-16)`.
- Pin checklist: weekly review dates, link to simili-bot comments filter, link to this plan.
- Use the issue for weekly notes so the audit trail is public and greppable.

### Step 4: Weekly triage review (x4)

- Once per week, list simili-bot's comments from the last 7 days.
- For each: mark useful / neutral / noise in the tracking issue.
- Flag any collisions or user confusion with triage bot comments.

### Step 5: Close the trial

- Tally against success criteria.
- Write `docs/decisions/007-simili-bot-trial.md` with outcome (adopt / extend / drop) and rationale.
- Update `docs/plans/2026-03-04-roadmap.md` — move step 38 from "next" to "done" or "evaluated and dropped", and adjust the "What We're Not Doing" section to reference the result.
- If adopting: add a short note to the README's limitations section ("duplicate detection handled by simili-bot, not this bot").

## Risks and Mitigations

- **Two bots commenting on the same issue is confusing.** Mitigation: similarity-only mode keeps simili-bot's output to a single "related issues" comment with clear attribution. Monitor user reactions in the weekly review; if confusion is observed, pause the trial.
- **Gemini quota contention.** Mitigation: use a separate API key for simili-bot so the triage bot's `MAX_DAILY_LLM_CALLS` budget is not eroded. Watch both dashboards weekly.
- **Simili-bot suggests already-closed duplicates as "related".** Usually fine, but can be noisy. Mitigation: note in weekly review; tune threshold if needed.
- **Sunk-cost pressure to adopt even if metrics are weak.** Mitigation: pre-committed numeric thresholds in Success Criteria above. If they miss, document and drop.

## References

- Roadmap entry: `docs/plans/2026-03-04-roadmap.md:322`
- Lean pivot that removed Phase 3: `docs/plans/2026-03-15-lean-bot-pivot-design.md`
- Decision context: `docs/decisions/004-lean-bot-pivot.md`
- Simili-bot upstream: https://github.com/similigh/simili-bot
