# ADR-007: simili-bot trial — interim outcome and reconfiguration

**Status:** In progress (mid-trial reconfiguration)
**Date:** 2026-04-26

## Context

The 2026-03-15 lean pivot removed Phase 3 (duplicate detection) from this bot on the basis that duplicate detection is a commodity feature better handled by an external tool. The trial plan at `docs/plans/2026-04-16-simili-bot-trial.md` proposed installing simili-bot (https://github.com/similigh/simili-bot) on `IsmaelMartinez/teams-for-linux` in `similarity-only` mode for 30 days, measuring against pre-committed precision, coverage, noise-floor, and cost criteria, and writing up the result here.

simili-bot was installed and has been running. A mid-trial review on 2026-04-26 looked at the 16 reports it produced between 2026-03-26 and 2026-04-26 and pulled the surrounding maintainer and OP responses for each. That sample is the basis for this interim record.

## Mid-trial review

Two findings drove the interim decision.

The first is configuration drift. The plan called for `similarity-only` mode "no auto-close, no auto-label". The reports landing on the repo today carry a "Possible Duplicate (Confidence: NN%)" callout, a "This issue will be automatically closed in 72 hours if no objections are raised" countdown, and a Quality Score / Quality Improvements section that overlaps with this bot's Phase 1. That is the full triage mode, not similarity-only. So the numbers below describe a more aggressive bot than the trial intended to measure.

The second is that against the as-installed configuration, two of the four success criteria fail. Of seven duplicate-flag calls, one (#2433, system tray three-dots) was correctly identified as a duplicate of #2090 and explicitly accepted by the OP. Six were wrong and prompted explicit "this is not a duplicate", "auto-close warning can be safely ignored", or "apologies for the noise" maintainer replies on the public thread (#2399, #2436, #2447, #2453, #2457, #2465). That is roughly 14% precision against a >60% target. Across all 16 reports, the maintainer posted at least five explicit "ignore the bot" or "apologies for the noise" messages, which is roughly 31% noise floor against a <20% target. Coverage and cost were not separately measured because the precision and noise gates already failed.

The "Similar Threads" list — the only output the trial actually wanted — was useful in every case. #2433 specifically was unblocked by it.

## Decision

Reconfigure simili-bot in the teams-for-linux repo to true similarity-only output before deciding adopt vs drop. Keep the "Similar Threads" table because that is the genuinely useful signal. Remove the duplicate-confidence callout, the auto-close countdown, and the Quality Score / Quality Improvements sections. The reconfiguration is a workflow-level change in the teams-for-linux repo and is being handed off there as a separate task.

After the reconfiguration is in place, observe the next ~10 issues to confirm the noise has dropped, then return to this record with adopt / extend / drop. If simili-bot's configuration cannot be reduced to "Similar Threads only" without forking, drop the bot and note the reason here.

## Consequences

Simili-bot stays installed but its output collapses to a similar-threads list. The trial timeline extends past the original 30-day window because the first ~30 days were measuring a misconfigured tool. The decision to make this an interim record rather than a final one is deliberate — closing it out now would either be a premature drop or a premature adoption based on data that does not match the intended configuration.

This bot's Phase 1 stays the source of truth for missing-info detection. The duplicate-detection gap left by removing Phase 3 remains open until simili-bot's reconfigured behaviour is observed; if it does not work in the narrower mode either, the gap stays open and we accept that.

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
