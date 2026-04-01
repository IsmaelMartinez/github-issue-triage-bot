# Repository Strategist

A GitHub App that maintains institutional memory for software projects and uses it to provide doc-grounded triage and strategic intelligence. Currently deployed against [teams-for-linux](https://github.com/IsmaelMartinez/teams-for-linux).

When an issue is opened, the bot analyses it against your project's documentation — troubleshooting guides, ADRs, roadmap, research docs, upstream dependency releases — and posts a concise comment with relevant context. Weekly, a synthesis engine detects issue clusters, ADR drift, and upstream dependency impacts, posting strategic briefings for maintainer review. An MCP server exposes all data to Claude Code agents for deeper analysis.

## What It Does

The bot runs a multi-phase triage pipeline on every new issue:

Phase 1 (deterministic, no LLM) checks if bug reports are missing key information by parsing the issue body against your project's form template. Phase 2 (vector search + Gemini) searches troubleshooting docs, upstream release notes, and similar past issues using pgvector similarity, then uses Gemini to generate targeted suggestions. Phase 4a (enhancements only) searches roadmap items, ADRs, and research documents to surface existing context about feature requests.

All results are consolidated into a single comment with a compact footer and feedback link. When a shadow repo is configured, comments are posted there for maintainer review first; on `lgtm`, a curated summary is promoted to the public issue.

### Synthesis Engine

Three weekly analysers run via a cron workflow: cluster detection groups similar recent issues by embedding similarity to flag emerging patterns, drift detection identifies ADRs contradicted by merged PRs and stale roadmap items, and upstream impact analysis cross-references new dependency releases against project documentation. Findings are posted as `[Briefing]` shadow issues.

### Enhancement Research Agent

For enhancement issues, the bot creates a mirror in a shadow repo with a context brief (relevant ADRs, roadmap items, similar issues). The maintainer can reply `research` to trigger full Gemini-powered research synthesis, `use as context` to acknowledge, or `reject` to discard. All agent outputs pass through two safety layers (structural validator + LLM reviewer) before posting.

### MCP Server

A stdio JSON-RPC server (`cmd/mcp/main.go`) exposes four tools: `get_pending_triage`, `get_synthesis_briefing`, `get_report_trends`, and `get_health_status`. This enables Claude Code agents to query the bot's data alongside repo-butler's portfolio data for combined intelligence. See [ADR-013](docs/adr/013-mcp-server.md).

### Live Dashboard

The dashboard at the Cloud Run URL shows triage activity, phase hit rates, reaction metrics, synthesis findings, and agent session status.

## Getting Started

### Prerequisites

Go 1.26+, Docker (for local PostgreSQL), a GCP project with Cloud Run, and a Neon PostgreSQL database with pgvector.

### 1. Install the GitHub App

Register a GitHub App with permissions: Issues (read/write), Contents (read/write), Pull requests (read/write). Subscribe to Issues and Issue comments webhook events. Set the webhook URL to your Cloud Run URL + `/webhook`.

### 2. Configure butler.json

Add `.github/butler.json` to your repository to control which capabilities are enabled:

```json
{
  "enabled": true,
  "capabilities": {
    "triage": true,
    "research": true,
    "synthesis": true,
    "auto_ingest": true
  },
  "doc_paths": ["docs/**", "*.md"],
  "shadow_repo": "owner/shadow-repo",
  "max_daily_llm_calls": 50
}
```

Without this file, the bot defaults to triage only (no synthesis, no auto-ingest).

### 3. Seed Documentation

The bot needs your project's documentation in the vector store:

```bash
go run ./cmd/seed troubleshooting path/to/troubleshooting-index.json
go run ./cmd/seed features path/to/feature-index.json
go run ./cmd/seed upstream path/to/upstream-releases.json
```

After the initial seed, the webhook handler keeps issue data up to date. If `auto_ingest` is enabled, documentation changes pushed to the default branch are automatically re-embedded.

### 4. Deploy

```bash
# Set secrets in GCP Secret Manager or as environment variables
# DATABASE_URL, GEMINI_API_KEY, GITHUB_APP_ID, GITHUB_PRIVATE_KEY, WEBHOOK_SECRET

# Deploy via Terraform
cd terraform && terraform apply

# Or build and deploy manually
docker build --platform linux/amd64 -t your-registry/server:latest .
```

Pushes to `main` automatically build and deploy via GitHub Actions using Workload Identity Federation.

### 5. Connect the MCP Server

```bash
claude mcp add triage-bot -- TRIAGE_BOT_URL=https://your-cloud-run-url go run ./cmd/mcp
```

## Development

```bash
go test ./...          # Run all tests
go vet ./...           # Static analysis
golangci-lint run ./...  # Linter (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8)

docker-compose up -d   # Local PostgreSQL with pgvector
go run ./cmd/server    # Run locally (set env vars first)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development setup and code style guidelines.

## Architecture

```
GitHub Webhooks ──→ Cloud Run (Go) ──→ Neon PostgreSQL + pgvector
                         │
                         ├── Triage Pipeline (Phase 1 + 2 + 4a)
                         ├── Enhancement Research Agent
                         ├── Synthesis Engine (weekly cron)
                         ├── Event Journal + Auto-Ingest
                         ├── Shadow Repo Approval Gates
                         ├── MCP Server (stdio JSON-RPC)
                         └── Live Dashboard
```

The bot is one layer in a four-layer stack: GitHub Agentic Workflows (operational chores) → Claude Code agents (combined intelligence) → [repo-butler](https://github.com/IsmaelMartinez/repo-butler) (portfolio orchestration) → this bot (institutional memory). See the [roadmap](docs/plans/2026-03-04-roadmap.md) for the full design.

## Documentation

- [Architecture Decision Records](docs/adr/) — 13 ADRs documenting key technical choices
- [Roadmap](docs/plans/2026-03-04-roadmap.md) — six streams covering feedback, quality, communication, strategic intelligence, agentic integration, and productionisation
- [Agentic Integration Design](docs/superpowers/specs/2026-04-01-agentic-integration-design.md) — Claude Code agent layer consuming both this bot and repo-butler via MCP
- [CLAUDE.md](CLAUDE.md) — comprehensive project context for AI coding assistants

## License

[MIT](LICENSE)
