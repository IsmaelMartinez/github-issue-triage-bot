# ADR-007: simili-bot trial — dropped

**Status:** Closed (dropped)
**Date:** 2026-04-26 (updated 2026-05-27)

## Context

The 2026-03-15 lean pivot removed Phase 3 (duplicate detection) from this bot on the basis that duplicate detection is a commodity feature better handled by an external tool. The trial plan at `docs/plans/2026-04-16-simili-bot-trial.md` proposed installing simili-bot (https://github.com/similigh/simili-bot) on `IsmaelMartinez/teams-for-linux` in `similarity-only` mode for 30 days, measuring against pre-committed precision, coverage, noise-floor, and cost criteria, and writing up the result here.

simili-bot was installed and has been running. A mid-trial review on 2026-04-26 looked at the 16 reports it produced between 2026-03-26 and 2026-04-26 and pulled the surrounding maintainer and OP responses for each. That sample is the basis for this interim record.

## Mid-trial review

Two findings drove the interim decision.

The first is configuration drift. The plan called for `similarity-only` mode "no auto-close, no auto-label". The reports landing on the repo today carry a "Possible Duplicate (Confidence: NN%)" callout, a "This issue will be automatically closed in 72 hours if no objections are raised" countdown, and a Quality Score / Quality Improvements section that overlaps with this bot's Phase 1. That is the full triage mode, not similarity-only. So the numbers below describe a more aggressive bot than the trial intended to measure.

The second is that against the as-installed configuration, two of the four success criteria fail. Of seven duplicate-flag calls, one (#2433, system tray three-dots) was correctly identified as a duplicate of #2090 and explicitly accepted by the OP. Six were wrong and prompted explicit "this is not a duplicate", "auto-close warning can be safely ignored", or "apologies for the noise" maintainer replies on the public thread (#2399, #2436, #2447, #2453, #2457, #2465). That is roughly 14% precision against a >60% target. Across all 16 reports, the maintainer posted at least five explicit "ignore the bot" or "apologies for the noise" messages, which is roughly 31% noise floor against a <20% target. Coverage and cost were not separately measured because the precision and noise gates already failed.

The "Similar Threads" list — the only output the trial actually wanted — was useful in every case. #2433 specifically was unblocked by it.

## Interim decision (2026-04-26)

Reconfigure simili-bot in the teams-for-linux repo to true similarity-only output before deciding adopt vs drop. Keep the "Similar Threads" table because that is the genuinely useful signal. Remove the duplicate-confidence callout, the auto-close countdown, and the Quality Score / Quality Improvements sections.

## Post-reconfiguration observation (2026-04-27 to 2026-05-18)

The reconfiguration shipped on 2026-04-27 with an explicit `steps:` list in `.github/simili.yaml` that bypassed the broken workflow-name handling. Between 2026-04-27 and 2026-05-18, simili-bot posted 24 reports across newly opened issues — all correctly in similarity-only mode with no duplicate callouts, no auto-close countdowns, and no quality score sections. The noise problems from the first 30 days were eliminated.

User engagement with the reports was minimal. One user (jayenashar on #2512) explicitly followed up on the similar-threads list and used it to disambiguate linked issues — the single strongest signal in the trial. The maintainer referenced similar threads from the report in replies on #2499 and #2531. No user or maintainer ever explicitly said the simili report was helpful or reacted to one. The reports were passively useful as background context but did not change the outcome of any issue conversation beyond the two cited cases.

## Final decision: drop (2026-05-27)

The simili-bot trial concludes with a drop verdict. The upstream repository (`similigh/simili-bot`) returned 404 as of 2026-05-20, causing all subsequent workflow runs to fail with "Repository access blocked". This makes continued operation impossible regardless of the tool's value. Even before the upstream disappearance, the evidence was marginal: 24 reports with two cases of explicit engagement is not strong enough to justify maintaining a forked copy of a disappeared dependency.

The duplicate-detection gap left by Phase 3 removal stays open. The team accepts this rather than pursuing a replacement immediately. The similar-threads use case is partially covered by this bot's own vector-search neighbours in `/brief-preview` and the `teams-for-linux-issue-review` skill, which already surfaces related issues with richer context (including in-flight PRs and upstream cross-references).

## Consequences

The `simili.yml` and `simili-seed.yml` workflows and `.github/simili.yaml` config should be removed from `IsmaelMartinez/teams-for-linux`. The Qdrant Cloud instance (if still active) can be decommissioned. No code changes needed in this repo.

## Learnings worth keeping regardless of final outcome

Verify configuration matches the plan before measuring. The first 30 days of this trial were spent collecting data about a more aggressive bot than the plan called for; the precision and noise numbers above describe that misconfigured state, not the intended one. A short configuration check at install time would have caught this immediately.

Auto-close countdowns inside maintainer-active threads land badly even when the duplicate call is correct. They add friction for the reporter and require the maintainer to write a "you can ignore this" comment alongside their substantive reply.

Stacking two bots that each comment on missing information creates conflicting signals to reporters. simili-bot's "Quality Score / Quality Improvements" overlapped with this bot's Phase 1, so reporters were getting two different framings of the same gap. If two tools cover the same job, one of them should be silent.

OP pushback ("Not a duplicate") is a strong evaluation signal in itself. When both the OP and the maintainer reject the same call (#2447 was the clearest instance), that is near-certain noise without needing further measurement.

The "Similar Threads" list in isolation has clear value. Every report's similar-threads section was either accurate context or harmless. The case for keeping simili-bot in some form rests entirely on that section.

## References

- Trial plan: `docs/plans/2026-04-16-simili-bot-trial.md`
- Lean pivot that removed Phase 3: `docs/plans/2026-03-15-lean-bot-pivot-design.md`
- Roadmap entry: `docs/plans/2026-03-04-roadmap.md` step 38
- simili-bot upstream: https://github.com/similigh/simili-bot
- Reconfiguration prompt handed off to the teams-for-linux repo: see chat log on 2026-04-26
