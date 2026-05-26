# Contributing

Thank you for your interest in contributing to the GitHub Issue Triage Bot.

## Development Setup

You need Go 1.26+ and a PostgreSQL instance with pgvector. The easiest way to get a local database is Docker Compose:

```bash
docker-compose up -d
```

Set the required environment variables (see CLAUDE.md for the full list):

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/triagebot"
export GEMINI_API_KEY="..."
export GITHUB_APP_ID="..."
export GITHUB_PRIVATE_KEY="..."   # base64-encoded PEM
export WEBHOOK_SECRET="..."
# SHADOW_REPOS is no longer used (retired by ADR 014 — silent RAG mode)
```

Build and run:

```bash
go build -o server ./cmd/server
./server
```

## Running Tests

All tests must pass before submitting a PR:

```bash
go test ./...
go vet ./...
```

Tests use Go's built-in testing package with table-driven patterns. There are no mocking frameworks — keep it simple.

## Code Style

The project follows standard Go conventions. A few specifics:

- Use `fmt.Errorf("operation: %w", err)` to wrap errors with context.
- Keep external dependencies minimal — prefer the standard library.
- The Gemini API client uses plain HTTP rather than an SDK.
- Phase 1 is pure string parsing with no network calls; keep it that way.

## Project Architecture

The triage pipeline runs in phases when a new GitHub issue is opened:

- Phase 1 (parsing): Checks for missing information in bug reports by matching template headers.
- Phase 2 (vector search + LLM): Finds similar troubleshooting docs, upstream release notes, and past issues, then generates solution suggestions.
- Phase 4a (vector search + LLM): For enhancements, finds related roadmap/ADR/research context.

For enhancement issues, the webhook embeds the issue and stores context silently (silent RAG mode, per ADR 014). Maintainers retrieve context on demand via the `/brief-preview` endpoint or the `teams-for-linux-issue-review` Claude Code skill. Shadow repo posting and agent sessions were retired — see `docs/adr/014-retire-shadow-repos.md`.

All phase results are consolidated into a single comment by `internal/comment/builder.go`.

## Seeding Data

The bot needs seeded data for vector search. Use the seed CLI to import JSON indexes:

```bash
go run ./cmd/seed troubleshooting <path-to-index.json>
go run ./cmd/seed issues <path-to-index.json>
go run ./cmd/seed features <path-to-index.json>
```

### butler.json Configuration

Each monitored repository can optionally include a `.github/butler.json` file to control which capabilities the bot exercises and how it behaves. When the file is absent the bot applies sensible defaults; when present, any field you set overrides the corresponding default while the rest remain unchanged.

The top-level `enabled` field is a boolean kill switch. Omitting it (or setting it to `null`) is treated as `true`; setting it to `false` disables all bot activity on the repository immediately. The `capabilities` object selects individual features: `triage` and `research` are on by default, while `synthesis`, `auto_ingest`, and `code_navigation` are off. The `doc_paths` array contains glob patterns (e.g. `"docs/**"`, `"*.md"`) that tell the auto-ingest pipeline which files to embed when a push lands on the default branch. The `upstream` array lists external dependencies to track, each with a `repo` (owner/repo format), `doc_type` (e.g. `"upstream_release"`), and `track` value (e.g. a major version like `"39"`).

The `synthesis` object controls weekly or monthly briefing generation with a `frequency` (`"weekly"` or `"monthly"`) and a `day` (any lowercase day of the week). The `shadow_repo` string (owner/repo format) is no longer used for posting — shadow repo delivery was retired by ADR 014. The field is retained for backward compatibility but ignored at runtime. The `thresholds` map lets you override per-document-type relevance thresholds (float values between 0.0 and 1.0) used during vector search. Finally, `max_daily_llm_calls` caps the number of Gemini API calls per day (default 50).

A minimal example enabling triage and synthesis on a repository:

```json
{
  "enabled": true,
  "capabilities": { "triage": true, "synthesis": true },
  "doc_paths": ["docs/**", "*.md"],
  "synthesis": { "frequency": "weekly", "day": "monday" },
  "upstream": [
    { "repo": "electron/electron", "doc_type": "upstream_release", "track": "39" }
  ],
  "thresholds": { "troubleshooting": 0.70, "adr": 0.55 },
  "max_daily_llm_calls": 50
}
```

The bot validates the config on load and emits warnings for common mistakes: using an unrecognised frequency or day value, threshold values outside the 0.0-1.0 range, malformed glob patterns in `doc_paths`, and `max_daily_llm_calls` exceeding the Gemini free-tier limit of 250. Validation issues are logged as warnings rather than hard errors, so the bot will still start with the defaults for any misconfigured fields.

## Submitting Changes

1. Fork the repo and create a feature branch.
2. Make your changes with clear commit messages.
3. Ensure `go test ./...` and `go vet ./...` pass.
4. Open a pull request against `main`.

CI runs tests automatically on every PR. Merges to `main` trigger deployment to Cloud Run via Terraform.
