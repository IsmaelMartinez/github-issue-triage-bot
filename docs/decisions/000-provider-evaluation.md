# Provider Evaluation — Step 0

Date: 2026-03-02

## LLM Provider: Gemini 2.5 Flash

Gemini 2.5 Flash was selected as the LLM provider. It is the only provider that offers a genuinely viable free tier for ongoing production use (250 requests/day), includes both generation and embeddings (`gemini-embedding-001`) under the same API key, and is already proven in the existing bot. At peak usage of 20 issues/day with 4-5 LLM calls each, daily cost on the free tier is $0.

Alternatives considered:

- OpenAI GPT-4o-mini: Cheaper per-token ($0.15/$0.60 vs $0.30/$2.50) but no ongoing free tier (only $5 credit that expires in 3 months). Best fallback if Gemini free tier ever becomes insufficient.
- Claude Haiku 4.5: No free tier, no embeddings API (requires separate Voyage AI integration), highest per-token cost ($1.00/$5.00). Poor fit for a cost-sensitive bot.
- Mistral Nemo: Cheapest per-token ($0.02/$0.04) with a free tier, but 2 RPM rate limit would serialize bot calls to 2-3 minutes per issue. Acceptable for async processing but adds unnecessary latency.
- Groq (Llama 3.1 8B): Fastest inference (~300+ tok/s) but no embeddings API and limited structured output support. Would require a second provider for embeddings.

Embedding model: `gemini-embedding-001` (default 3072 dimensions, configurable down to 768). We will use 768 dimensions to minimize storage while maintaining good retrieval quality.

## Database Provider: Neon PostgreSQL (Free Tier)

Neon was selected for managed PostgreSQL with pgvector. The free tier provides 0.5 GB storage per project, 100 compute-unit hours per month (400 hours at 0.25 CU), and built-in PgBouncer connection pooling. pgvector is supported natively via `CREATE EXTENSION vector`.

The key trade-off is scale-to-zero after 5 minutes of inactivity, with ~500ms cold starts. This is acceptable for a webhook handler that processes requests asynchronously.

Alternatives considered:

- Supabase: Free tier pauses after 7 days of inactivity with 1-2+ minute resume times. For a bot that may go days without issues, this creates unacceptable cold start latency. Upgrade path is $25/month (vs Neon's $5/month).
- Cloud SQL (GCP): No free tier ($10+/month minimum). Overkill for this workload.
- Railway PostgreSQL: No permanent free tier (30-day trial only). $5/month after trial.
- CockroachDB: pgvector compatibility layer is relatively new and less battle-tested.

Connection string: Use the `-pooler` hostname variant to route through PgBouncer, since each webhook invocation creates a new connection.

## Hosting Provider: Google Cloud Run (Free Tier)

Cloud Run was selected for hosting the Go binary. The always-free tier includes 2 million requests/month, 180,000 vCPU-seconds/month, and 1 GiB outbound data. At 5-20 requests/day, utilization is negligible. Go cold starts on Cloud Run are 300ms-1s, well within the 5-second response target.

Deployment is standard Docker image pushed to Artifact Registry, deployed via `gcloud run deploy`. Custom domains supported with automatic TLS.

Alternatives considered:

- Fly.io: No real free tier for new customers (2-hour trial only). ~$2-5/month minimum.
- Railway: $5/month Hobby plan. Good DX but not free.
- Render: Free tier has 30-second cold starts that would cause GitHub webhook timeouts (10s limit).
- Koyeb: Free tier includes always-on service + PostgreSQL. Good fallback option if GCP setup feels too heavy.
- AWS Lambda: Free tier is generous but operational complexity (API Gateway, IAM, VPC for DB access) is not justified for this simple service.
- Vercel: Requires restructuring Go code into function-per-endpoint pattern. Non-commercial restriction on free tier.

## Summary

| Component | Provider | Cost | Key Benefit |
|-----------|----------|------|-------------|
| LLM + Embeddings | Gemini 2.5 Flash | $0/month (free tier) | 250 RPD, proven in production |
| Database | Neon PostgreSQL | $0/month (free tier) | Native pgvector, built-in pooling |
| Hosting | Google Cloud Run | $0/month (free tier) | Fast Go cold starts, standard Docker |

Total monthly cost: $0 (within free tier limits for current usage profile).
