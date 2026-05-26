# Simplification and RAG Evolution

Date: 2026-05-26
Status: Draft
Scope: github-issue-triage-bot, triage-bot-test-repo, teams-for-linux-shadow

## Context

The triage bot was built with a shadow-repo approval gate: new issues generate triage comments in a private shadow repo, where the maintainer reviews and replies `lgtm` to promote them to the source issue. After three months of operation, a four-agent audit of the shadow repos revealed that 75% of shadow issues auto-closed without any maintainer review, and the /teams-for-linux-issue-review skill has become the primary triage interface, querying the bot's vector store directly via the `get_brief_preview` MCP tool without involving the shadow repos at all.

The triage-bot-test-repo (64 synthetic issues) and its companion shadow repo served their purpose during initial development but are no longer exercised. The bot's Electron upstream knowledge covers v39/v40/v41 but has no version lifecycle policy, no version-aware retrieval, and a hardcoded symptom keyword list that limits regression PR search.

This design covers two phases: simplifying the architecture by removing shadow posting and archiving dead repos, then evolving the RAG retrieval engine to be version-aware, recency-weighted, and LLM-driven.

## Phase 1: Simplification

### 1.1 Remove shadow posting

Remove the `TF_VAR_shadow_repos` environment variable from `.github/workflows/deploy.yml` (line 76). This is the sole control for shadow posting; without it, the `h.shadowRepos` map in the webhook handler is empty and all shadow-related code paths are skipped.

The webhook handler currently embeds every incoming issue into the vector store at line 417 of `internal/webhook/handler.go` (the `upsertIssue` call), before the shadow branching at line 531. This embedding continues to work regardless of shadow configuration.

### 1.2 Switch to silent RAG mode

With shadow posting removed, the bot should stop running the full triage pipeline (Phase 1 template parsing, Phase 2 vector search + LLM synthesis) on every incoming webhook. Instead, the webhook handler should embed the issue and return. The full triage pipeline only runs on demand when the skill calls `get_brief_preview`.

This means modifying the `handleIssueEvent` function in `handler.go` to call `upsertIssue` and then return early, skipping Phase 1/2/synthesis and comment posting. Remove the `/retriage` comment command handler entirely — it has no purpose in silent mode.

The `/brief-preview` endpoint and its MCP tool wrapper remain unchanged — they already run their own retrieval queries independent of the webhook pipeline.

Savings: eliminates per-issue Gemini API calls (Phase 1 JSON extraction, Phase 2 LLM scoring, synthesis generation). Only the embedding call (`llm.Embed`) runs per webhook.

### 1.3 Seed Electron v42

Run `scripts/generate-upstream-index.sh` for Electron v42 issues and releases. Commit the two JSON files under `data/electron-v42-issues.json` and `data/electron-v42-releases.json`. Trigger the seed workflow to load them into the vector store. Update `VERSIONS` in `.github/workflows/upstream-refresh.yml` to include v42 so the monthly refresh covers it.

### 1.4 Strip dashboard of shadow-specific metrics

The dashboard (`cmd/server/dashboard.go` + `cmd/server/template.html`) displays several shadow-specific panels that become dead with shadow posting removed:

Remove from `DashboardStats` in `internal/store/report.go`:
- `TriageStats` (shadow triage promoted/pending counts)
- `RecentTriage` (shadow issue links)
- `AgentStats` (shadow-linked agent sessions, `RecentAgent` with shadow references)
- `ApprovalGateStats` (lgtm/reject gate outcomes)

Keep:
- `TotalComments`, `TotalThumbsUp`, `TotalThumbsDown` (feedback on bot comments, still relevant if any historical data exists)
- `DocumentCounts` (vector store document inventory by type)
- `IssueCount` (embedded issues count)
- `PhaseHitRate` (which retrieval phases found results)
- `FeedbackStats`, `SynthesisStats` (quality metrics — historical data only; no new data generated in silent mode)
- `DailyTriageCounts` (volume chart, rename to daily embedding counts)
- `RoundTripDistribution` (response time percentiles)

Update `template.html` to remove the shadow-specific panels and simplify the layout. Remove the `DASHBOARD_REPOS` default that references the test repo (lines 9, 38, 52 of `dashboard.yml`); default to `IsmaelMartinez/teams-for-linux` only.

### 1.5 Simplify maintenance workflow

The daily maintenance workflow (`dashboard.yml`, currently named "Daily Maintenance") runs three steps: close stale shadow issues (`/cleanup`), health check, and reaction sync. Remove the stale-shadow-issue cleanup step since there will be no shadow issues. Keep health check and reaction sync.

### 1.6 Port edge-case test patterns

Before archiving the test repo, verify that the following edge cases from its issue corpus are covered in existing Go unit test fixtures under `internal/phases/phase1_test.go`:

- Non-English issue body (Spanish, from test issue #59)
- Injection payloads in issue body (script tags, javascript: URIs, data: URIs, from test issues #17 and #18)
- Enhancement request parsed through bug template (scoring 0/4 with missing bug-report fields, from test issues #30, #42)

Add any missing fixtures before archiving.

### 1.7 Archive repos

After Phase 1 ships and is verified:

- Archive `IsmaelMartinez/triage-bot-test-repo`
- Archive `IsmaelMartinez/triage-bot-test-repo-shadow`
- Archive `IsmaelMartinez/teams-for-linux-shadow`

This is a manual step via GitHub Settings > Archive. The bot's config no longer references any of these repos after the deploy.yml and dashboard.yml changes.

## Phase 2: RAG Evolution

### 2.1 Version lifecycle system

Add an `electron_version` field to the seed metadata at embed time. When `cmd/seed` processes an upstream doc, it should extract the major version from the document title or source metadata and write it to the `metadata` JSONB column as `"electron_version": "41"`.

Build a `DeleteDocumentsByVersion(ctx, repo, docTypes []string, version string) error` method in `internal/store/postgres.go` that deletes all documents matching the given doc types and version from the metadata field.

Wire version cleanup into the monthly `upstream-refresh.yml` workflow. After seeding the current active versions, the workflow should call a new `./seed cleanup` subcommand that queries the store for all distinct Electron versions, compares them against the active set, and deletes any version more than 2 majors behind the highest active version. With teams-for-linux on v41 and v42 stable, the active set would be {v40, v41, v42} and v39 would be dropped.

Re-seed existing v39/v40/v41 documents with the `electron_version` metadata field so the cleanup logic can find them. This can be done by re-running the seed workflow for those versions after the metadata change is deployed.

### 2.2 Version-aware retrieval boosting

When `get_brief_preview` runs, extract an Electron version from the issue body if present. The issue template's debug output section typically contains the Electron version (from `teams-for-linux --version` or `package.json`). Use a regex like `[Ee]lectron[\s:]+v?(\d+)` to extract the major version.

In the document retrieval query within `internal/mcp/tools/brief_preview.go`, add a post-retrieval rerank step (similar to hat-aware boosting in `internal/hats/boost.go`): documents whose `electron_version` metadata matches the extracted version get a distance reduction of 0.05. Documents from adjacent versions (N-1 or N+1) get a smaller reduction of 0.02. This is a soft rerank, not a filter.

### 2.3 Recency weighting on similar-issue queries

The `FindSimilarIssues` method in `internal/store/postgres.go` currently orders results purely by cosine distance. Add a time-decay factor: for each returned issue, compute a recency bonus based on its `created_at` timestamp. Issues from the last 6 months get a distance reduction of 0.05, issues from 6-12 months get 0.02, and older issues get no bonus. Apply the bonus after the vector search (post-retrieval rerank, not in SQL) to avoid complicating the pgvector index scan.

This prevents stale issues with strong keyword overlap from outranking recent issues with the same root cause. The bonus values should be configurable via constants at the top of the file.

### 2.4 LLM-based symptom keyword extraction

Replace the hardcoded keyword list in `extractSymptomKeywords` (in `internal/mcp/tools/brief_preview.go` or the regression PR search logic) with an LLM call using Gemini Flash. The prompt should ask the model to extract 3-8 technical keywords from the issue body that would identify relevant PRs: component names, API names, error messages, platform-specific terms, Electron subsystem names.

Use the existing `llm.Provider.GenerateJSON` method with a structured output schema: `{"keywords": ["wayland", "screen-share", "desktopCapturer"]}`. Temperature 0.1, max tokens 256. Cache the extraction result per issue to avoid redundant LLM calls.

Fall back to the existing hardcoded keyword matching if the LLM call fails or returns an empty list.

## Files affected

### Phase 1
- `.github/workflows/deploy.yml` — remove `TF_VAR_shadow_repos`
- `.github/workflows/dashboard.yml` — remove test repo from `DASHBOARD_REPOS`, remove cleanup step
- `internal/webhook/handler.go` — simplify to embed-only on webhook
- `internal/store/report.go` — remove shadow-specific stats structs
- `cmd/server/dashboard.go` — no changes (template handles removed fields)
- `cmd/server/template.html` — remove shadow panels, simplify layout
- `internal/phases/phase1_test.go` — add edge-case fixtures if missing
- `data/electron-v42-issues.json` — new file (generated by script)
- `data/electron-v42-releases.json` — new file (generated by script)
- `.github/workflows/upstream-refresh.yml` — add v42 to `VERSIONS`

### Phase 2
- `cmd/seed/main.go` — add `electron_version` to metadata, add `cleanup` subcommand
- `internal/store/postgres.go` — add `DeleteDocumentsByVersion` method
- `internal/store/models.go` — document the version metadata field
- `internal/mcp/tools/brief_preview.go` — version-aware boosting, LLM keyword extraction
- `internal/store/postgres.go` — recency bonus in `FindSimilarIssues` (post-retrieval)
- `.github/workflows/upstream-refresh.yml` — add cleanup step after seeding

## Out of scope

- Multi-model fallback (using Claude Sonnet/Opus as escalation tier for complex issues). Worth exploring separately but orthogonal to this design.
- MCP keyword search tool for upstream issues (noted in the test repo's improvement prompt as a separate deliverable).
- Webhook catch-up mechanism for missed deliveries. The backfill tool exists for manual recovery; a scheduled re-sync is a separate feature.
- Curated triage example bank (persisting skill draft vs. maintainer final comment diffs as RAG context). This is the natural next step after Phase 2 but requires changes to the skill, not just the bot.
