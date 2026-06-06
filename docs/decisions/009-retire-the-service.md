# ADR-009: Retire the Repository Strategist service — sunsetting a successful experiment

Status: Accepted (wind-down decided; execution per `docs/plans/2026-06-05-retire-service-keep-capability.md`, not yet scheduled)
Date: 2026-06-05
Resolves: ADR-008 (service vs. tools — previously "under discussion")

## Context

This repository began as an issue-triage bot and evolved through a long sequence of deliberate pivots — dark factory, lean bot, repository strategist, silent RAG mode, skill-driven triage. Every pivot did the same structural thing: it moved reasoning out of the Go service and into the agent layer, until the service's remaining job was retrieval and weekly synthesis. ADR-008 framed the open question — whether a bespoke service was still justified, or whether a composition of off-the-shelf tools plus the existing Claude skill could do the job — and set a measurement gate rather than deciding on taste.

That measurement has now run. Once the skill's retrieval wiring was repaired, the `teams-for-linux-issue-review` skill was used on roughly a dozen real issues over the following week, and a transcript-mining pass turned "is it useful?" into a number: about a third of the bot's similar-issue suggestions were cited in the resulting drafts, with a strongly bimodal shape — genuinely additive on recurring auth and login themes, and adding nothing the draft used on novel issues. Every other capability showed near-zero consumption: the weekly synthesis findings were glanced at on the dashboard with no evidence they were ever acted on, the `/report/trends` endpoint built for a programmatic consumer had no reads, the upstream-candidates field returned the same content regardless of input, and the learnings field returned generic label entries unrelated to the diagnosis. Against that thin, narrow value sat a standing Cloud Run service, a Neon Postgres database, CI, and Terraform — and a recurring maintenance tax that the final weeks of the project spent paying down (an embedding leak, a dead metric, documentation drift, a misrouted MCP registration), all on a service handling a handful of calls a day.

Then the landscape moved. On 2026-05-20 GitHub shipped native semantic issue search in Copilot Chat (generally available on all Copilot plans, backed by a real semantic issues index) alongside semantic code search in the Copilot coding agent. The one capability ADR-008 had called hard to buy — longitudinal semantic search over a single repo's issue history — became a commodity, covering both halves of our useful slice with no operational surface on our side. The only present limitation is that the semantic issues index is reachable from the Copilot Chat web UI rather than from a public API or the GitHub MCP server, so it cannot yet be called programmatically from the skill.

## Decision

Retire the bespoke service. Dissolve the Cloud Run deployment, the Neon database, and the surrounding CI and Terraform, and re-home the single capability that earned its keep — semantic recall over the issue history — into `teams-for-linux` as a thin local embedding index the skill calls directly, exactly as set out in the 2026-06-05 implementation plan. That local index is a deliberate bridge: GitHub's native semantic search is the strategic destination, and the bridge is kept intentionally thin so that when GitHub exposes the index through an API or MCP tool, the skill swaps to it and the local index is deleted. Codenav is not rebuilt; the skill reads the local checkout and Copilot's semantic code search covers the rest. The off-the-shelf full-triage product (Dosu) was considered and rejected as the wrong shape — it auto-labels and replies, which contradicts the silent-mode posture, and is enterprise contact-priced. With the service gone, the long-deferred rename (roadmap Stream 7) resolves itself: the repository is archived or reduced to the home of the indexing script.

This is recorded deliberately as the retirement of a successful experiment, not the failure of one. The experiment was productive precisely because it ran far enough to produce evidence, and the right time to close an experiment is when it has answered its question.

## What the experiment proved — learnings harvested

The value of this project is the body of learnings it produced about where intelligence should live in a maintainer's workflow, and those are worth recording even as the code is retired.

On RAG: semantic vector recall genuinely surfaced old, differently-worded issues that keyword search misses — the one durable win — but RAG over the small documentation corpus, a few dozen files that fit comfortably in an agent's context window, was over-engineering that an agent simply reading `docs/` does better. On shadow repos: mirroring issues into a private repository to give an agent a workspace was heavy infrastructure whose real value turned out to be the analysis it produced rather than the mirror itself, which is why it was retired in ADR-014. On bot promotions and reply drafting: the maintainer-style reply drafter matured best as a Claude Code skill in the agent layer, not as logic embedded in the service. On dashboards: a live charts dashboard gave glanceable status but was viewed only occasionally and never drove a decision, a reminder that a dashboard nobody acts on is vanity. On observability and trend reporting: the structured `/report/trends` endpoint built for a programmatic consumer went unread because that consumer was never wired up, teaching that the producer should not be built before the consumer. On silent mode: replacing automatic public comments with on-demand retrieval matched what the maintainer actually wanted — judgement in the loop rather than auto-posted bot noise. And on measurement discipline: instrumenting real use before deciding is what turned a vague "is this useful?" into a concrete usage rate, and that number is what finally grounded this retirement rather than intuition.

The throughline across all of them is the same conclusion every pivot pointed at: reasoning belongs in the agent and the skill, durable memory belongs in a tiny local index, and the heavy always-on service in between was scaffolding the workflow outgrew.

## Consequences

The maintainer keeps the capability that worked, in a form with no servers, no database, and no monthly cost, callable from the skill today and swappable for GitHub's native search tomorrow. The recurring maintenance burden disappears, along with the budget for Cloud Run and Neon. The synthesis engine, dashboard, health monitor, event journal, and the already-retired agent and mirror subsystems are wound down; their findings were not consumed, so nothing downstream breaks. Decommissioning starts by uninstalling the GitHub App and removing its webhook from teams-for-linux so issue events stop reaching a dead endpoint, and includes removing the now-defunct `triage-bot` MCP registration that points at the retired Cloud Run URL. The Git history is preserved as the record of the experiment. The risks and rollback path are covered in the implementation plan: the service can be left scaled-to-zero rather than destroyed until the local path is proven on a few live issues, and reverting is a single skill edit. Execution is not yet scheduled; this ADR records the decision and the reasoning so the wind-down proceeds deliberately.

## References

- ADR-008 (service vs. tools, now resolved): `docs/decisions/008-service-vs-tools.md`
- Implementation plan: `docs/plans/2026-06-05-retire-service-keep-capability.md`
- Silent mode: `docs/decisions/002-silent-mode.md`
- Shadow-repo pattern and its retirement: `docs/decisions/003-shadow-repo-pattern.md`, `docs/adr/014-retire-shadow-repos.md`
- Weekly synthesis engine: `docs/decisions/006-weekly-synthesis-engine.md`
- simili-bot trial (a prior buy-vs-build call): `docs/decisions/007-simili-bot-trial.md`
- GitHub native semantic issue search (2026-05-20): github.blog/changelog/2026-05-20-semantic-issue-search-in-copilot-chat
