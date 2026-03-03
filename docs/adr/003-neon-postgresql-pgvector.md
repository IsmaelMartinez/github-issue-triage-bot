# ADR 003: Neon PostgreSQL with pgvector for Vector Storage

## Status

Implemented

## Context

The triage bot needs a database for three purposes: storing document embeddings for similarity search (troubleshooting guides, roadmap items, ADRs), storing issue embeddings for duplicate detection, and tracking bot comments for accuracy reporting. Vector similarity search is the core retrieval mechanism, replacing the old approach of stuffing entire JSON indexes into LLM prompts.

The database must support pgvector for cosine similarity search on 768-dimensional float32 vectors, handle concurrent reads from webhook processing, and cost nothing during development and early production.

## Decision

Use Neon PostgreSQL 17 on the free tier with pgvector 0.8.0, hosted in `aws-us-east-2`. The database is managed outside Terraform (Neon has no official Terraform provider). Connection pooling uses Neon's built-in PgBouncer via the `-pooler` hostname variant.

The schema has three tables: `documents` (documentation chunks with embeddings), `issues` (issue summaries with embeddings), and `bot_comments` (tracking table). Vector columns use `vector(768)` type with `ivfflat` indexes using cosine distance (`vector_cosine_ops`). The `lists` parameter for ivfflat indexes is set based on the square root rule: `sqrt(N)` rounded up, adjusted as the dataset grows.

The Go driver is `pgx/v5` with `pgvector-go` for vector type encoding.

## Consequences

### Positive

pgvector is native PostgreSQL — no separate vector database to manage. Neon's free tier provides 0.5 GB storage and built-in connection pooling, which is sufficient for the current dataset (18 docs + ~1,400 issues = ~8 MB of vector data). Scale-to-zero saves cost during inactive periods. The pooler hostname handles connection lifecycle transparently.

### Negative

Neon scales to zero after 5 minutes of inactivity, with ~500ms cold starts on the database side. Combined with Cloud Run's cold start, the first request after inactivity can take 1-2 seconds. This is acceptable since webhook processing is asynchronous (the handler returns 202 immediately and processes in a goroutine).

The ivfflat index requires manual tuning of the `lists` parameter as data grows. We maintain a separate migration file (`002_update_ivfflat_lists.sql`) for this. HNSW indexes would be self-tuning but use more memory.

### Neutral

Neon being managed outside Terraform means the database lifecycle is manual. This is a trade-off of using a provider without Terraform support, but the database rarely changes after initial setup.

## Alternatives Considered

### Supabase

Free tier pauses after 7 days of inactivity with 1-2+ minute resume times. For a bot that may go days without issues, this creates unacceptable cold start latency.

Why rejected: Pause/resume behavior incompatible with webhook responsiveness.

### Cloud SQL (GCP)

No free tier ($10+/month minimum). Native GCP integration would simplify networking but the cost is not justified for this workload.

Why rejected: No free tier.

### Pinecone / Weaviate / Qdrant (dedicated vector DBs)

Purpose-built vector databases with managed hosting options.

Why rejected: Adding a separate vector database alongside PostgreSQL (still needed for bot_comments tracking) doubles operational complexity. pgvector handles the workload at this scale.

## Related

- Database schema: `migrations/001_initial.sql`
- Index tuning: `migrations/002_update_ivfflat_lists.sql`
- Store implementation: `internal/store/postgres.go`
- Models: `internal/store/models.go`
