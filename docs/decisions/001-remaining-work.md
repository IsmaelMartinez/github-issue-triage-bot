# Remaining Work — Deployment and Cutover

Date: 2026-03-02
Updated: 2026-03-03

This document captures the concrete next steps to go from "code compiles and tests pass locally" to "bot is triaging issues in production on teams-for-linux".

## Completed

Steps marked with [x] are done.

- [x] Provision Neon database (project: falling-resonance-06310725, pgvector 0.8.0, migration applied)
- [x] Deploy to Google Cloud Run via Terraform (service URL: https://triage-bot-lhuutxzbnq-uc.a.run.app)
- [x] Configure webhook on test repo (IsmaelMartinez/triage-bot-test-repo)
- [x] Create billing budget (GBP 15/month, alerts at 5%, 25%, 50%)
- [x] Infrastructure as code (terraform/main.tf manages APIs, Artifact Registry, Cloud Run, IAM, budget)

## Next: Seed database

The bot is deployed and receiving webhooks but has no data to search against. The Gemini API key must also be configured.

```bash
# 1. Get a Gemini API key from https://aistudio.google.com/apikeys
# 2. Update terraform.tfvars with the key
# 3. Run terraform apply to update Cloud Run env vars

# 4. Seed the database
export DATABASE_URL="$(neonctl connection-string --project-id falling-resonance-06310725 --pooled)"
export GEMINI_API_KEY="<key>"

go build -o seed ./cmd/seed

./seed troubleshooting ../teams-for-linux/.github/issue-bot/troubleshooting-index.json
./seed issues ../teams-for-linux/.github/issue-bot/issue-index.json
./seed features ../teams-for-linux/.github/issue-bot/feature-index.json
```

Note: seeding issues will make ~200 embedding API calls. At free tier rate limits, this may take ~20 minutes. The seed tool needs rate limiting added.

## Next: Validate on test repo

Create test issues on `triage-bot-test-repo`:

1. A bug with all fields filled in (should get solution suggestions + duplicate check)
2. A bug with missing reproduction steps and debug output (should get missing info checklist)
3. A bug that's PWA-reproducible (should get PWA note)
4. An enhancement request (should get context matches from roadmap/ADRs)
5. A misclassified issue (bug labeled as enhancement, should get relabel suggestion)

Verify the bot comments appear within ~5 seconds and match expected format.

## Remaining steps

### CI/CD

Set up GitHub Actions workflow in `github-issue-triage-bot` that runs tests on PRs and builds/deploys on push to main.

### Seed tool improvements

The seed CLI needs rate limiting for Gemini embedding API calls, progress reporting, idempotent operation (skip items with existing embeddings), and a --dry-run flag.

### Cut over to teams-for-linux

Configure webhook on teams-for-linux, run both bots in parallel briefly, then remove old bot workflows and scripts.

### Accuracy reporting

Implement a /report endpoint or CLI command that queries bot_comments table for reaction tallies.

## Infrastructure reference

| Resource | Value |
|---|---|
| GCP project | gen-lang-client-0421325030 |
| Cloud Run URL | https://triage-bot-lhuutxzbnq-uc.a.run.app |
| Artifact Registry | us-central1-docker.pkg.dev/gen-lang-client-0421325030/triage-bot |
| Neon project | falling-resonance-06310725 |
| Neon region | aws-us-east-2 |
| Billing budget | GBP 15/month |
| Webhook secret | stored in terraform.tfvars (never committed) |
| Test repo webhook | hook ID 598755550 |
