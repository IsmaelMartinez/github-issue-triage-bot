# Remaining Work — Deployment and Cutover

Date: 2026-03-02
Updated: 2026-03-03

This document captures the concrete next steps to go from "code compiles and tests pass locally" to "bot is triaging issues in production on teams-for-linux".

## Completed

- [x] Provision Neon database (project: falling-resonance-06310725, pgvector 0.8.0, migration applied)
- [x] Deploy to Google Cloud Run via Terraform (service URL: https://triage-bot-lhuutxzbnq-uc.a.run.app)
- [x] Configure webhook on test repo (IsmaelMartinez/triage-bot-test-repo, hook ID 598755550)
- [x] Create billing budget (GBP 15/month, alerts at 5%, 25%, 50%)
- [x] Infrastructure as code (terraform/main.tf manages APIs, Artifact Registry, Cloud Run, IAM, budget)
- [x] Seed database (18 troubleshooting/config docs, 111 issues, features index)
- [x] Configure Gemini API key and SOURCE_REPO env var via Terraform
- [x] Validate on test repo: bot correctly posts triage comments with Phase 1 (missing info), Phase 2 (solution suggestions), Phase 3 (duplicate detection), Phase 4b (misclassification check)
- [x] Fix maxOutputTokens for Gemini 2.5 Flash thinking model (500/400/1024 → 8192)
- [x] Fix SOURCE_REPO override for testing against different repo's data
- [x] Migrate Terraform state to GCS backend (gs://triage-bot-terraform-state)
- [x] Set up Workload Identity Federation for GitHub Actions (keyless auth)
- [x] Create CI/CD workflow (.github/workflows/deploy.yml) with SHA-based image tags
- [x] Add README and project documentation

## Remaining steps

### Data seeding

Seed all 1,356 issues (currently only 111 from static index) and the feature index into the database. See docs/plans/2026-03-03-next-phase-design.md for the data strategy.

### Public dashboard

Build a static dashboard showing bot activity, feedback metrics, and knowledge base stats. See docs/plans/ for details.

### Cut over to teams-for-linux

Configure webhook on teams-for-linux, run both bots in parallel briefly, then remove old bot workflows and scripts. When cutting over, remove or clear SOURCE_REPO (it won't be needed since webhook repo = data repo).

### GitHub integration for teams-for-linux

Design and implement a GitHub App or Action that teams-for-linux uses to connect to this service. Needs thorough testing on triage-bot-test-repo first.

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
| Terraform state | gs://triage-bot-terraform-state (GCS, versioned) |
| CI/CD | GitHub Actions, image tags: sha-{7char} |
| WIF pool | projects/62054333602/.../github-actions |
| Deploy SA | triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com |
| Test repo webhook | hook ID 598755550 |
