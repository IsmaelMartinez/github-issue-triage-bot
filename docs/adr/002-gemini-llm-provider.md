# ADR 002: Gemini 2.5 Flash as LLM Provider

## Status

Implemented

## Context

The triage bot needs two LLM capabilities: text generation (for analyzing issues, comparing duplicates, checking misclassification) and text embeddings (for vector similarity search). We evaluated five providers on cost, free tier viability, embedding support, and structured output quality.

The bot processes 5-20 issues per day, each requiring 4-5 LLM calls plus 1 embedding call. The system must run at zero cost during development and early production.

## Decision

Use Gemini 2.5 Flash for both generation and embeddings, accessed via the REST API directly (no SDK) to minimize dependencies.

Generation uses the `gemini-2.5-flash:generateContent` endpoint with `responseMimeType: application/json` for structured output. All phases use `maxOutputTokens: 8192` to accommodate the model's internal reasoning tokens (Gemini 2.5 Flash is a "thinking model" where reasoning consumes the output token budget).

Embeddings use `gemini-embedding-001` at 768 dimensions (configurable down from the default 3072) to balance storage efficiency with retrieval quality.

## Consequences

### Positive

The free tier (250 requests/day, 1,500 RPM) covers production usage with significant headroom. A single API key provides both generation and embeddings, simplifying configuration. The structured JSON output mode eliminates most response parsing issues. Cost at current usage is $0/month.

### Negative

Gemini 2.5 Flash's "thinking model" behavior means internal reasoning tokens consume the `maxOutputTokens` budget. We discovered this when phases set to 400-500 tokens produced truncated JSON responses. The fix (increasing to 8192) works but means the model uses more tokens than strictly necessary for the output.

Gemini's free tier has a 250 requests/day hard limit. If the bot ever processes more than ~50 issues/day, it would exceed this limit. The paid tier is inexpensive ($0.30/1M input tokens) but requires billing setup.

### Neutral

The REST API approach means we handle HTTP requests manually rather than using an SDK. This is more code (~160 lines in `internal/llm/client.go`) but avoids a dependency and gives full control over request construction.

## Alternatives Considered

### OpenAI GPT-4o-mini

Cheaper per-token but no ongoing free tier (only a $5 credit that expires in 3 months). Would be the best fallback if Gemini's free tier becomes insufficient.

Why rejected: No sustainable free tier for an open-source project bot.

### Claude Haiku 4.5

No free tier, no embeddings API (would require separate Voyage AI integration), highest per-token cost among options evaluated.

Why rejected: Cost and the need for a second provider for embeddings.

### Mistral Nemo

Cheapest per-token ($0.02/$0.04) with a free tier, but the 2 RPM rate limit would serialize bot calls to 2-3 minutes per issue.

Why rejected: Rate limit makes it impractical for responsive triage.

## Related

- Provider evaluation: `docs/decisions/000-provider-evaluation.md`
- LLM client: `internal/llm/client.go`
- maxOutputTokens fix: all phase files in `internal/phases/`
