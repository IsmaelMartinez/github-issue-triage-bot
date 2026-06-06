# Plan: retire the bespoke service, keep the useful slice (move into teams-for-linux)

Status: Proposed (planning only — no code or infra changes made yet)
Date: 2026-06-05
Relates to: `docs/decisions/008-service-vs-tools.md` (ADR-008), roadmap Stream 7 (rename), the real-use findings recorded in agent memory.

## Decision in one paragraph

The measured real use of `/brief-preview` says the bespoke service has one capability worth keeping — semantic recall over the full issue history (and, secondarily, codenav) — and that everything else (synthesis findings, the upstream-candidates and learnings fields, the dashboard, the health monitor, the retired agent and mirror code) is unconsumed. Across ~12 distinct real issues the usage proxy held steady at roughly a third of the bot's similar-issue suggestions being cited in drafts, strongly bimodal: materially additive on recurring auth/login-themed issues, zero on novel ones. The plan is therefore to dissolve the standing Cloud Run + Neon service and re-home its one useful capability inside `teams-for-linux` as a thin, no-ops local tool the `teams-for-linux-issue-review` skill calls directly, built deliberately small so it can be swapped for GitHub's new native semantic issue search once that exposes an API.

## What changed in the landscape (the headline)

The thing ADR-008 called the irreducible, hard-to-buy core — longitudinal semantic search over a single repo's issue history — stopped being hard to buy. On 2026-05-20 GitHub shipped semantic issue search in Copilot Chat, generally available on all Copilot plans and powered by a dedicated semantic issues index, and earlier shipped semantic code search inside the Copilot coding agent. Between them they cover both halves of our useful slice natively, with zero operational surface on our side. The one limitation, confirmed against the changelog and the GitHub MCP server's toolset docs, is that the semantic issues index is reachable only from Copilot Chat on the web today; there is no documented REST, GraphQL, or MCP surface, and the GitHub MCP server's `issues` toolset still uses the keyword search API. So a Claude Code skill cannot call GitHub's semantic index programmatically yet — but the direction of travel is unmistakable, and GitHub historically exposes UI features through APIs over time. This reframes the whole question: we are no longer deciding whether to maintain a unique capability forever, only how to bridge the gap until the commodity version is callable from where we need it.

## The off-the-shelf options, weighed

GitHub native semantic issue and code search is the strategic destination. It is free on existing Copilot plans, maintained by GitHub, and is exactly the capability. Its only gap is programmatic access from the skill, which is a question of timing, not feasibility. The maintainer can already use it manually today by asking Copilot Chat for issues similar to the one under triage, as a cross-check.

Dosu is the closest off-the-shelf full-triage product (auto-labelling, deduplication, and replies driven by a semantic layer). It is the wrong shape for this project: it posts automatically, which conflicts directly with the silent-mode posture (ADR-014) and the maintainer's explicit preference to keep their judgement in the loop; its pricing is enterprise contact-sales; and it would replace the skill rather than feed it. Notably, even Dosu is moving toward less hosted machinery and more in-repo control (its `better-stale-bot` direction), which validates the dissolve-the-service instinct rather than contradicting it.

A local committed embedding index is the pragmatic bridge that works inside the skill today. The issue corpus is small (~1,400 embedded issues out of ~2,600 issue numbers), so semantic search over it is a few megabytes and a brute-force cosine — no database, no service. The maintainer already runs local embedding models through the `delegate-local` skill (`embed.sh` / `semantic-search.sh`), so the whole capability can run on-device with no API key and no recurring cost.

Keeping the bespoke service is rejected by the data and the maintenance ledger: this very stretch of work has been spent fixing an embedding leak, a dead metric, doc drift, and a misrouted MCP registration on a service handling a handful of calls a day and then none for a day.

## Recommended approach: two tracks

Track one, now: replace the service's role with a local embedding index committed to `teams-for-linux`, consumed by the skill's step 1c, and retire Cloud Run + Neon. Build it deliberately thin — a single search entry point with a stable contract — so that track two is a cheap swap rather than a rewrite.

Track two, strategic: treat GitHub native semantic search as the eventual backend. The moment GitHub exposes the semantic issues index via API or the official MCP server, the skill's step 1c switches from the local script to that call and the committed index is deleted. Until then, the maintainer uses Copilot Chat semantic search manually as a parallel sanity check. Codenav is not rebuilt at all: the skill already has the repo checked out locally and can grep and read it, and Copilot semantic code search covers the rest.

## Implementation steps (track one)

1. Re-home, do not migrate, the data. The issue corpus is public and re-fetchable via `gh`, so regenerate embeddings from scratch with the local model rather than exporting the Neon pgvector table. This drops the Gemini dependency entirely and avoids a fragile migration.
2. Add a small `issue-recall/` tool to `teams-for-linux` (exact location TBD with the maintainer — likely under an existing scripts/tools dir). It contains: a committed, compressed embeddings file (`{number, title, state, vector}` for all issues, float16 to keep it a few MB); a `search` entry point that embeds the query with the same local model, computes cosine similarity, and prints the top-k similar issues in a stable format; and a `reindex` entry point that fetches issues via `gh`, embeds new or changed ones, and rewrites the committed file.
3. Pin one embedding model for both index and query (cosine is only valid if both sides use the same model). Use the `delegate-local` embedding tier so it stays on-device; document the model name so a future model change triggers a full reindex.
4. Refresh cadence: a maintainer-run `reindex` (matching the keep-it-manual preference) rather than an always-on automation, optionally backed by a scheduled GitHub Action later if staleness becomes annoying. Re-embedding only the delta keeps it cheap.
5. Rewire the skill. Change step 1c in `teams-for-linux-issue-review/SKILL.md` from the `curl .../brief-preview` call to invoking the local `search` entry point, keeping the same downstream contract (a list of similar issues the draft can cite). This is the only change to the skill's behaviour; the rest of the hat logic is unaffected.
6. Decommission the service once the local path is proven on a few live issues, in this order: first uninstall the GitHub App and remove its webhook from `teams-for-linux` (so issue events stop POSTing to a soon-dead endpoint), then `terraform destroy` the Cloud Run service, Artifact Registry image, budget, and secrets; delete the Neon project; remove the GitHub Actions deploy/seed/synthesis/event-ingest workflows; and remove the now-defunct `triage-bot` MCP registration from the local Claude config (it points at the retired Cloud Run URL). Keep the Git history.
7. Resolve the identity question (roadmap Stream 7) as a consequence: with the service gone, `github-issue-triage-bot` is either archived read-only or reduced to the home of the indexing script; the rename becomes moot or trivial.

## Migration, rollback, and risks

Rollback during the bridge is simple because the service can be left scaled-to-zero (not destroyed) until the local path has handled a handful of live issues; if the local index underperforms, step 1c reverts to the `curl` call by reverting one skill edit. The main risk is embedding-quality parity: the local model's neighbours may differ from Gemini's, so the first few reindexed results should be spot-checked against the recorded dataset (the same auth/login issues that scored well before should still surface). A second, smaller risk is index staleness between manual reindexes; this is acceptable because the highest-value matches are old recurring issues that do not change, and the freshest issues are exactly the ones a plain `gh search` already catches. The codenav decision carries effectively no risk since nothing is built.

## Open decisions for the maintainer

Whether to run the local index on the `delegate-local` on-device model (zero cost, zero external dependency, reindex must run on the maintainer's machine) or on a hosted embedder like Gemini called only at reindex and query time (no standing service, but a re-introduced API dependency). The recommendation is the on-device model, for full alignment with the no-ops goal. Also open: whether to destroy the service immediately after the bridge is proven or leave it scaled-to-zero for a grace period; and where exactly the tool should live inside `teams-for-linux`.

## What this plan is not

It does not commit any code or infrastructure change. It records the recommended path and the concrete steps so the dissolve-and-rehome can be executed deliberately, and so the eventual switch to GitHub's native semantic search is a planned swap rather than a surprise.
