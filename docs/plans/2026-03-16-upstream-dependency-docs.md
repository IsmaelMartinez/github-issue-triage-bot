# Upstream Dependency Docs in Vector Store

Date: 2026-03-16
Status: Future (not started)

## Summary

Seed Electron release notes, breaking changes, and version-tagged issues into the vector store so the bot can surface upstream context for bug reports. Two use cases: helping users with bugs caused by known Electron issues, and re-checking open issues against new Electron release notes when upgrading.

## Data Sources

Electron release notes: fetch from `gh api repos/electron/electron/releases` for versions 39 and 40. Tag with `doc_type: "upstream_release"`, `upstream_repo: "electron/electron"`, `electron_version: "39"`.

Electron issues by version: fetch from `gh api repos/electron/electron/issues` filtered by milestone (`39-x-y`, `40-x-y`). Tag with `doc_type: "upstream_issue"`. This gives a manageable set (hundreds, not thousands).

Chromium flags: lower priority. The Electron docs list supported flags per version. Could seed as `doc_type: "upstream_config"`.

## Implementation

New seed script `scripts/generate-electron-index.sh` that fetches release notes and milestone-filtered issues from the Electron repo, produces a seed JSON compatible with `./seed features`. Per-category threshold for upstream docs: 45-50% (contextual background, not direct solutions).

Phase 2 search scope: widen `FindSimilarDocuments` call to include upstream doc types, or add a second search pass. The LLM prompt already handles mixed doc types — it just needs to know that upstream matches are informational context, not direct solutions.

## Version Lifecycle

When upgrading Electron (e.g. 39 → 40): seed Electron 40 release notes and issues. Keep Electron 39 docs for a grace period (useful for users on older versions). Drop docs for versions more than 2 major versions behind. Tag all upstream docs with `electron_version` in metadata for filtering.

## Re-check Open Issues

After seeding new release notes, run backfill against open issues to see if any match newly-fixed upstream bugs. Surface matches on the dashboard or create shadow issues for maintainer review.

## Trigger to Start

The Electron 40 migration (already planned in the roadmap). That's the natural moment to seed upstream docs since the release notes will be relevant immediately.
