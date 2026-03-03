# ADR 001: Standalone Go Service for Issue Triage

## Status

Implemented

## Context

The issue triage bot started as inline JavaScript embedded in a GitHub Actions workflow within the teams-for-linux repository. Over time it grew to a 940-line YAML file with 4 Node.js scripts (930 LOC), 3 JSON data indexes (89 KB), and 4 supporting workflows. This architecture had several problems: inline JavaScript in YAML is untestable, static JSON indexes require noisy PRs to update, there are no unit tests, and the system is tightly coupled to one repository.

We needed to decide whether to continue evolving the in-repo approach, extract it to a standalone service, and if so, what language and architecture to use.

## Decision

Extract the triage bot into a standalone Go service in its own repository (`github-issue-triage-bot`). The service receives GitHub webhook events via HTTP, runs a multi-phase triage pipeline backed by vector similarity search and LLM generation, and posts consolidated markdown comments via the GitHub API.

Go was chosen for the service language because it produces small, statically-linked binaries (the server binary is ~10 MB), has fast cold starts on serverless platforms (300ms-1s on Cloud Run), has excellent standard library support for HTTP servers and JSON handling, and the team has Go experience from other projects. The only external dependencies are `pgx/v5` (PostgreSQL driver) and `pgvector-go`.

The separate repository gives the bot its own release cycle, test suite, and CI/CD pipeline independent of teams-for-linux.

## Consequences

### Positive

The codebase went from untestable YAML/JS to structured Go packages with table-driven tests. Phase 1 alone has 18 unit tests. The static JSON indexes (89 KB total stuffed into Gemini prompts) were replaced by vector embeddings in PostgreSQL, cutting token usage and improving match quality. Real-time issue updates via webhooks eliminate the need for index regeneration workflows. The service can eventually serve multiple repositories.

### Negative

There is now a second repository to maintain and deploy. The infrastructure cost is higher in terms of setup complexity (Cloud Run, Neon, Terraform) even though the monetary cost is zero on free tiers. The migration from the old bot requires a transition period where both systems run in parallel.

### Neutral

The Go codebase is roughly the same total LOC as the old Node.js scripts, but organized into testable packages rather than inline YAML.

## Alternatives Considered

### Keep inline JS in GitHub Actions

Continue the existing approach with incremental improvements.

Why rejected: The fundamental problems (untestable, no database, noisy index updates) are architectural and cannot be solved within the GitHub Actions YAML format.

### Node.js standalone service

Extract to a standalone service but keep the existing Node.js code.

Why rejected: Go offers better cold start performance on serverless platforms, smaller binary size, and the team wanted to use Go for new infrastructure tooling.

### Python standalone service

Use Python with FastAPI or similar.

Why rejected: Slower cold starts, larger container images, and runtime dependency management (pip/venv) adds operational complexity compared to a single Go binary.

## Related

- Initial plan: `docs/plans/` (the plan file from the parent teams-for-linux repo)
- Provider evaluation: `docs/decisions/000-provider-evaluation.md`
- Project structure: `cmd/server/`, `internal/`, `migrations/`
