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
export SILENT_MODE="false"          # optional, default "true" (observer mode)
export SHADOW_REPOS="owner/repo:owner/shadow"  # optional, comma-separated mappings
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
- Phase 2 (vector search + LLM): Searches all document types (troubleshooting, configuration, ADR, roadmap, research) using vector similarity and generates targeted suggestions with links.
- Phase 4a (vector search + LLM): For enhancements, finds related roadmap/ADR/research context.

For enhancement issues with a configured shadow repo, the bot also starts an agent session. The agent creates a mirror issue in a private shadow repository and posts a context brief with relevant ADRs, roadmap items, and similar past issues. The maintainer can reply `research` to trigger full Gemini research synthesis, `use as context` to acknowledge, or `reject` to discard. All agent outputs pass through two safety layers — a structural validator and an LLM reviewer — and the agent escalates to a human after 4 round-trips without reaching review. See `internal/agent/` for the implementation and `docs/decisions/` for the relevant ADRs.

All phase results are consolidated into a single comment by `internal/comment/builder.go`.

## Seeding Data

The bot needs seeded data for vector search. Use the seed CLI to import JSON indexes:

```bash
go run ./cmd/seed troubleshooting <path-to-index.json>
go run ./cmd/seed issues <path-to-index.json>
go run ./cmd/seed features <path-to-index.json>
```

## Submitting Changes

1. Fork the repo and create a feature branch.
2. Make your changes with clear commit messages.
3. Ensure `go test ./...` and `go vet ./...` pass.
4. Open a pull request against `main`.

CI runs tests automatically on every PR. Merges to `main` trigger deployment to Cloud Run via Terraform.
