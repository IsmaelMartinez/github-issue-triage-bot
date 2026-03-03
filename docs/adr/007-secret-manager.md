# ADR 007: Google Secret Manager for Runtime Secrets

## Status

Implemented

## Context

The bot requires several sensitive credentials at runtime: DATABASE_URL, GEMINI_API_KEY, GITHUB_PRIVATE_KEY, WEBHOOK_SECRET, and GITHUB_APP_ID. Initially these were passed as plain Cloud Run environment variables, which meant they were visible in the GCP console's Cloud Run configuration page and stored in plaintext within the Terraform state file. Anyone with `run.services.get` permission could read them, and a compromised state file would leak all secrets.

## Decision

Migrate all secrets to Google Secret Manager, referenced by Cloud Run via `secret_key_ref` in the container spec. Each secret is a first-class Secret Manager resource managed by Terraform. IAM bindings follow the principle of least privilege: only the Cloud Run service account receives `roles/secretmanager.secretAccessor` on each individual secret, rather than a project-wide binding.

Terraform manages the secret resource lifecycle (create, update, destroy) and the IAM bindings, but secret values are set via `terraform.tfvars` which is gitignored.

## Consequences

### Positive

Secrets are no longer visible in the Cloud Run environment variable listing in the GCP console. Secret Manager provides an audit trail of access via Cloud Audit Logs, making it possible to detect unauthorized reads. Per-secret IAM bindings mean compromising one secret's access does not grant access to others. Secret versioning allows safe rotation without downtime.

### Negative

Slightly more Terraform complexity — each secret requires a `google_secret_manager_secret` resource, a `google_secret_manager_secret_version` resource, and an `google_secret_manager_secret_iam_member` binding. The Secret Manager API must be enabled on the GCP project. Cold starts may be marginally slower due to secret resolution, though in practice this is negligible.

### Neutral

The application code itself does not change. Cloud Run injects secrets as environment variables at container startup, so the Go code continues reading `os.Getenv()` as before. The change is entirely at the infrastructure layer.

## Related

- Terraform config: `terraform/main.tf` (secret resources and IAM bindings)
- Cloud Run service account: `triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com`
- ADR 005: Terraform with GCS State Backend
