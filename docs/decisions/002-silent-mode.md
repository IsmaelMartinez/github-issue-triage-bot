# Silent Mode — Observe Before Acting

Date: 2026-03-04
Status: Superseded by [ADR-003](003-shadow-repo-pattern.md) (2026-03-05)

> **Note (2026-04-14):** The `SILENT_MODE` environment variable and the `triage_results` draft-comment table were removed when the shadow-repo review gate landed (migration `008_drop_triage_results.sql`). All triage and agent output now flows through a shadow repository instead. This record is retained for historical context only.

## Context

The triage bot is receiving negative reactions ("bad AI", "useless AI") on teams-for-linux, even for Phase 1 output that is pure template parsing with no AI involvement. The problem is perception: any bot comment is assumed to be AI-generated, and if the suggestion isn't perfectly relevant, it confirms the "useless AI" narrative. The 2025 Stack Overflow survey found developer trust in AI accuracy at 29%, meaning the bot is fighting an uphill battle with every public comment it posts.

The Enhancement Researcher agent already operates in a private shadow repo and doesn't post publicly until "publish" is explicitly approved. The triage pipeline, however, posts immediately to every new issue. This asymmetry needs correcting — the triage side should earn trust through demonstrated quality before resuming public output.

## Decision

Switch the triage pipeline to silent observation mode. The bot continues processing every issue through all phases (Phase 1 through 4b), continues storing embeddings and updating the issue database, and continues starting agent sessions in shadow repos. The difference is that triage comments are stored in a new `triage_results` table as draft comments instead of being posted to GitHub.

The maintainer reviews drafts via the dashboard's "Silent Triage Results" section, which shows each issue number, title, phases that ran, a summary of what each phase found (e.g. "3 missing fields, 2 duplicates"), and the full draft comment text.

Silent mode is controlled by the `SILENT_MODE` environment variable, which defaults to `"true"`. Setting it to `"false"` restores the original behavior where comments are posted publicly. This is a deployment-time toggle, not a per-issue or per-phase control.

## Consequences

The bot stops producing public-facing noise. Negative reactions cease because there are no public comments to react to. The triage pipeline continues running at full fidelity, accumulating data about which phases produce consistently useful output for which issue types. This data — visible in the dashboard — informs the decision about when and which phases to re-enable publicly.

The trade-off is that users get no automated triage help during the silent period. Maintainers must manually review the dashboard to benefit from the bot's analysis. This is acceptable because the current public comments were net-negative for user perception.

Agent sessions in shadow repos are unaffected. The `bot_comments` table is unchanged and retains historical data from the pre-silent era. The `sync-reactions` tool continues to operate on previously posted comments.

When silent mode is eventually disabled (for all phases or selectively per-phase), comment framing improvements (problem-first language, less bot-like tone) should be applied simultaneously to avoid repeating the perception problem.

## Implementation

Migration `006_triage_results.sql` creates the `triage_results` table with columns for repo, issue_number, issue_title, draft_comment, phases_run (text array), and phase_details (JSONB). The table has a unique constraint on `(repo, issue_number)` so re-processing an issue upserts rather than duplicating.

The webhook handler's `handleOpened` method checks `silentMode` after building the comment. In silent mode, it calls `RecordTriageResult` instead of `CreateComment` and `RecordBotComment`. The dedup check also queries `HasTriageResult` in silent mode to prevent re-processing issues that were already triaged silently.

The dashboard stats (`GetDashboardStats`) include `total_drafts` and `recent_drafts` from the `triage_results` table. The HTML template renders a "Silent Triage Results" section that is hidden when there are no drafts.

## Files Changed

| File | Change |
|---|---|
| `migrations/006_triage_results.sql` | New table |
| `internal/store/models.go` | `TriageResultRecord` struct |
| `internal/store/postgres.go` | `RecordTriageResult`, `HasTriageResult`, `GetRecentTriageResults` |
| `internal/store/report.go` | `TotalDrafts`, `RecentDrafts` in dashboard stats |
| `internal/webhook/handler.go` | `silentMode` field, conditional posting, `buildPhaseDetails` |
| `cmd/server/main.go` | `SILENT_MODE` env var parsing |
| `cmd/dashboard/template.html` | "Silent Triage Results" section |
| `CLAUDE.md` | `SILENT_MODE` env var documentation |

## Future Work

Per-phase silent mode: allow enabling public comments for specific phases (e.g. Phase 1 only) while keeping others silent. This requires extending `SILENT_MODE` from a boolean to a comma-separated list of phases to silence, or inverting it to a list of phases to publish.

Draft publishing: add a dashboard action or CLI command to manually publish a stored draft comment to GitHub. This lets the maintainer review and selectively post the most useful drafts.
