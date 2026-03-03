# Remaining Work — Deployment and Cutover

Date: 2026-03-02
Updated: 2026-03-03
Status: Complete

All items are now complete. The bot is running in production on teams-for-linux as a GitHub App.

## Completed

- [x] Provision Neon database (project: falling-resonance-06310725, pgvector 0.8.0, migrations applied)
- [x] Deploy to Google Cloud Run via Terraform (service URL: https://triage-bot-lhuutxzbnq-uc.a.run.app)
- [x] Configure webhook on test repo (IsmaelMartinez/triage-bot-test-repo)
- [x] Create billing budget (GBP 15/month, alerts at 5%, 25%, 50%)
- [x] Infrastructure as code (terraform/main.tf manages APIs, Artifact Registry, Cloud Run, IAM, budget, secrets)
- [x] Seed database (18 troubleshooting/config docs, 1,356 issues with embeddings)
- [x] Configure Gemini API key via Secret Manager
- [x] Validate on test repo: all phases working correctly
- [x] Fix maxOutputTokens for Gemini 2.5 Flash thinking model
- [x] Migrate Terraform state to GCS backend (gs://triage-bot-terraform-state)
- [x] Set up Workload Identity Federation for GitHub Actions (keyless auth)
- [x] Create CI/CD workflow (.github/workflows/deploy.yml) with SHA-pinned actions
- [x] Security hardening: API key in header, body size limit, non-root Docker, error body caps, embedding validation
- [x] Migrate secrets to Google Secret Manager
- [x] Prompt injection defenses: system instructions separated from user content, LLM output sanitization
- [x] Webhook replay protection via delivery ID tracking
- [x] Pin GitHub Actions to commit SHAs
- [x] Public dashboard with daily generation workflow and GitHub Pages deployment
- [x] Convert to GitHub App authentication (ghinstallation, installation-scoped tokens)
- [x] Production cutover: GitHub App installed on teams-for-linux

## Infrastructure reference

| Resource | Value |
|---|---|
| GCP project | gen-lang-client-0421325030 |
| Cloud Run URL | https://triage-bot-lhuutxzbnq-uc.a.run.app |
| Artifact Registry | us-central1-docker.pkg.dev/gen-lang-client-0421325030/triage-bot |
| Neon project | falling-resonance-06310725 (aws-us-east-2) |
| Database | PostgreSQL 17 + pgvector 0.8.0 |
| Billing budget | GBP 15/month |
| Terraform state | gs://triage-bot-terraform-state (GCS, versioned) |
| CI/CD | GitHub Actions (.github/workflows/deploy.yml) |
| Auth | GitHub App with installation-scoped tokens |
| Dashboard | Daily generation, deployed to GitHub Pages |
