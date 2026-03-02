# Remaining Work — Deployment and Cutover

Date: 2026-03-02

This document captures the concrete next steps to go from "code compiles and tests pass locally" to "bot is triaging issues in production on teams-for-linux".

## Prerequisites

These tools need to be installed/authenticated before starting:

- `neonctl` — Neon CLI (`brew install neonctl`, then `neonctl auth`)
- `gcloud` — Google Cloud CLI (`brew install --cask google-cloud-sdk`, then `gcloud auth login`)
- `docker` — for building/pushing images
- `psql` — for running migrations (`brew install libpq`)

## Step 6a: Provision Neon database

```bash
# Create project
neonctl projects create --name github-issue-triage-bot --region-id aws-us-east-2

# Get project ID from output, then create database
neonctl databases create --name triage_bot --project-id <PROJECT_ID>

# Get connection string
neonctl connection-string --project-id <PROJECT_ID>

# Run migration
psql "$(neonctl connection-string --project-id <PROJECT_ID>)" -f migrations/001_initial.sql

# Verify pgvector is enabled
psql "$(neonctl connection-string --project-id <PROJECT_ID>)" -c "SELECT extversion FROM pg_extension WHERE extname = 'vector';"
```

## Step 6b: Seed database with existing indexes

```bash
export DATABASE_URL="$(neonctl connection-string --project-id <PROJECT_ID>)"
export GEMINI_API_KEY="<key>"

# Build seed tool locally
go build -o seed ./cmd/seed

# Seed from teams-for-linux repo
./seed troubleshooting ../teams-for-linux/.github/issue-bot/troubleshooting-index.json
./seed issues ../teams-for-linux/.github/issue-bot/issue-index.json
./seed features ../teams-for-linux/.github/issue-bot/feature-index.json
```

Note: seeding issues will make ~200 embedding API calls. At free tier rate limits (10 RPM for Gemini), this takes ~20 minutes. The seed tool should be updated to add rate limiting / batching.

## Step 6c: Deploy to Google Cloud Run

```bash
# Create GCP project (or use existing)
gcloud projects create triage-bot-prod --name="Issue Triage Bot"
gcloud config set project triage-bot-prod
gcloud billing projects link triage-bot-prod --billing-account=<BILLING_ACCOUNT_ID>

# Enable APIs
gcloud services enable run.googleapis.com artifactregistry.googleapis.com

# Create Artifact Registry repository
gcloud artifacts repositories create triage-bot \
  --repository-format=docker \
  --location=us-central1

# Build and push image
gcloud auth configure-docker us-central1-docker.pkg.dev
docker build -t us-central1-docker.pkg.dev/triage-bot-prod/triage-bot/server:v1 .
docker push us-central1-docker.pkg.dev/triage-bot-prod/triage-bot/server:v1

# Deploy
gcloud run deploy triage-bot \
  --image=us-central1-docker.pkg.dev/triage-bot-prod/triage-bot/server:v1 \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars="DATABASE_URL=<NEON_CONNECTION_STRING>,GEMINI_API_KEY=<KEY>,GITHUB_TOKEN=<TOKEN>,WEBHOOK_SECRET=<SECRET>"

# Get the service URL from the output
```

## Step 6d: Configure webhook on test repo

```bash
# Generate a webhook secret
WEBHOOK_SECRET=$(openssl rand -hex 20)

# Create webhook on test repo
gh api repos/IsmaelMartinez/triage-bot-test-repo/hooks \
  --method POST \
  --input - <<EOF
{
  "name": "web",
  "active": true,
  "events": ["issues"],
  "config": {
    "url": "https://<CLOUD_RUN_URL>/webhook",
    "content_type": "json",
    "secret": "$WEBHOOK_SECRET",
    "insecure_ssl": "0"
  }
}
EOF
```

## Step 6e: Validate on test repo

Create test issues on `triage-bot-test-repo`:

1. A bug with all fields filled in (should get solution suggestions + duplicate check)
2. A bug with missing reproduction steps and debug output (should get missing info checklist)
3. A bug that's PWA-reproducible (should get PWA note)
4. An enhancement request (should get context matches from roadmap/ADRs)
5. A misclassified issue (bug labeled as enhancement, should get relabel suggestion)

Verify the bot comments appear within ~5 seconds and match expected format.

## Step 7a: Add CI/CD to the triage bot repo

Set up GitHub Actions workflow in `github-issue-triage-bot` that:

1. Runs `go test ./...` on PRs
2. Runs `go vet ./...` and `golangci-lint`
3. On push to main: builds Docker image, pushes to Artifact Registry, deploys to Cloud Run

## Step 7b: Add seed tool improvements

The seed CLI needs:

1. Rate limiting for Gemini embedding API calls (10 RPM on free tier)
2. Progress reporting (X/Y items seeded)
3. Idempotent operation (skip items that already have embeddings)
4. A `--dry-run` flag

## Step 7c: Configure webhook on teams-for-linux

```bash
gh api repos/IsmaelMartinez/teams-for-linux/hooks \
  --method POST \
  --input - <<EOF
{
  "name": "web",
  "active": true,
  "events": ["issues"],
  "config": {
    "url": "https://<CLOUD_RUN_URL>/webhook",
    "content_type": "json",
    "secret": "$WEBHOOK_SECRET",
    "insecure_ssl": "0"
  }
}
EOF
```

Run both the old GitHub Actions bot and the new service in parallel for 1-2 weeks.

## Step 7d: Remove old bot from teams-for-linux

Once the new service is validated, remove from teams-for-linux:

- `.github/workflows/issue-triage-bot.yml`
- `.github/workflows/update-issue-index.yml`
- `.github/workflows/update-feature-index.yml`
- `.github/workflows/bot-accuracy-report.yml`
- `.github/issue-bot/` directory (scripts + JSON indexes)

Update the teams-for-linux roadmap to reflect the migration.

## Step 7e: Add accuracy reporting

Implement a `/report` endpoint or CLI command that queries `bot_comments` table for reaction tallies, replacing the `tally-bot-feedback.js` script.

## Future enhancements (not blocking cutover)

- Multi-repo support (change `repo` column default, add repo config table)
- Automatic document re-indexing when teams-for-linux docs change (via push webhook)
- Dashboard for monitoring bot accuracy over time
- Webhook retry handling (idempotency keys)
