# ADR 013: MCP Server for Agent Integration

## Status

Implemented (PR #92)

## Context

The triage bot exposes its data via HTTP endpoints (`/report`, `/report/trends`, `/health-check`) that repo-butler already consumes. As the project evolves toward an agentic integration layer (Stream 5), Claude Code agents need to query both the triage bot and repo-butler from a single interface. Repo-butler already ships an MCP server (Phase 7) for its portfolio data. The triage bot needed an equivalent so agents can consume both systems uniformly.

The alternatives considered were: exposing data only via HTTP (simpler, but agents would need custom HTTP tooling per endpoint and tool schemas wouldn't be discoverable), building a combined MCP server in a third project (adds a new codebase to maintain), or embedding the MCP server inside the existing Cloud Run HTTP server (conflates two protocols and makes the MCP server depend on a running database).

## Decision

Build a standalone MCP server (`cmd/mcp/main.go`) that connects to the triage bot's HTTP API as a client, not to the database directly. The MCP server uses the JSON-RPC 2.0 protocol over stdio, following the same pattern repo-butler established. Four tools wrap existing endpoints: `get_pending_triage` (wraps `/report`), `get_synthesis_briefing` and `get_report_trends` (wrap `/report/trends`), and `get_health_status` (wraps `/health-check`).

The HTTP-proxy approach was chosen over direct database access because it keeps the MCP binary stateless and decoupled from the database schema. The same binary works against the local dev server or the Cloud Run production URL, configured via a single `TRIAGE_BOT_URL` environment variable. Authentication uses the existing `INGEST_SECRET` bearer token.

The protocol implementation (`internal/mcp/`) is a reusable package separate from the tool definitions (`internal/mcp/tools/`), so new tools can be added without modifying the protocol code.

## Consequences

The MCP server fulfils the triage bot's half of the Phase 8 contract on repo-butler's roadmap (A2A Agent Card + Triage Bot Contract). Claude Code agents can now query triage bot data via `claude mcp add triage-bot -- go run ./cmd/mcp`. The four-layer architecture (GitHub Agentic Workflows, Claude Code agents, repo-butler, triage bot) now has typed MCP interfaces between the agent layer and both data sources.

The HTTP-proxy pattern means the MCP server adds latency (one extra HTTP hop) compared to direct database access, but this is acceptable for the agent use case where calls are infrequent (daily/weekly schedules, not real-time). If latency becomes a concern, the tool implementations can be swapped to direct store calls without changing the MCP protocol layer.

As part of this work, the `/report/trends` endpoint was enriched with structured synthesis findings (clusters, drift signals, upstream impacts) stored in event metadata by the synthesis runner. This benefits all consumers of the endpoint, not just the MCP server.

## References

- Design: `docs/superpowers/specs/2026-04-01-agentic-integration-design.md`
- Implementation plan: `docs/superpowers/plans/2026-04-01-triage-bot-mcp-server.md`
- Repo-butler interoperability ADR: `docs/decisions/003-interoperability-layer.md` (in repo-butler)
- Roadmap: `docs/plans/2026-03-04-roadmap.md` (Stream 5)
