# ADR 004: Google Cloud Run for Hosting

## Status

Implemented

## Context

The Go service needs a hosting platform that can receive HTTP webhook requests from GitHub, run for 5-30 seconds per request (the time to process a multi-phase triage pipeline), and scale to zero when idle. The bot receives 5-20 requests per day, so a permanently running server would waste resources.

## Decision

Deploy on Google Cloud Run (v2) in `us-central1` with `cpu_idle = true` (CPU is only allocated during request processing). The service is configured with 1 vCPU, 256 MB memory, max 1 instance, and 30-second request timeout. It allows unauthenticated access since GitHub webhooks cannot use GCP IAM authentication.

The Docker image is a multi-stage build: `golang:1.26-alpine` for compilation, `alpine:3.21` for runtime. The final image includes the server binary, seed CLI binary, and migration files.

Infrastructure is managed via Terraform (`terraform/main.tf`) with state stored in a GCS bucket (`gs://triage-bot-terraform-state`). CI/CD deploys automatically on push to main via GitHub Actions with Workload Identity Federation for keyless GCP authentication.

## Consequences

### Positive

The always-free tier (2M requests/month, 180K vCPU-seconds/month) covers production usage indefinitely at current volumes. Go cold starts are 300ms-1s, well within GitHub's 10-second webhook timeout. The service handles the webhook response in <1 second (returns 202 Accepted immediately, processes asynchronously in a goroutine). Terraform manages all GCP resources declaratively.

### Negative

Cloud Run requires Docker images in Artifact Registry, adding a build step. The max instance count of 1 means concurrent webhook deliveries would queue, but at 5-20 issues/day this is not a concern. Cold starts from both Cloud Run and Neon can stack to 1-2 seconds, though this only affects the first request after a period of inactivity.

### Neutral

The billing budget (GBP 15/month with alerts at 5%, 25%, 50%) provides a safety net against unexpected costs, though actual usage is well within free tier limits.

## Alternatives Considered

### Fly.io

No real free tier for new customers (2-hour trial only). Would cost $2-5/month minimum.

Why rejected: Cost, when Cloud Run provides equivalent functionality for free.

### Render

Free tier has 30-second cold starts that would cause GitHub webhook timeouts (10-second limit).

Why rejected: Cold start latency incompatible with webhook requirements.

### AWS Lambda

Generous free tier but operational complexity (API Gateway, IAM, VPC for database access) is not justified for a single-endpoint webhook handler.

Why rejected: Setup complexity disproportionate to the workload.

## Related

- Terraform config: `terraform/main.tf`
- Dockerfile: `Dockerfile`
- CI/CD workflow: `.github/workflows/deploy.yml`
- Provider evaluation: `docs/decisions/000-provider-evaluation.md`
