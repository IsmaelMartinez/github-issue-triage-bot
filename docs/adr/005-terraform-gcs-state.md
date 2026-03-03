# ADR 005: Terraform with GCS State Backend

## Status

Implemented

## Context

The project's GCP infrastructure (Cloud Run service, Artifact Registry, IAM bindings, billing budget) needs to be managed declaratively. The initial setup used Terraform with local state, which meant only one person could run `terraform apply`, there was no locking to prevent concurrent modifications, and losing the laptop would mean losing the state.

## Decision

Use Terraform (>= 1.5) with a GCS backend for state storage. The state bucket is `gs://triage-bot-terraform-state` in `us-central1` with object versioning enabled for rollback capability and uniform bucket-level access. GCS provides native state locking via object metadata, so no separate DynamoDB-style lock table is needed.

Terraform manages: Cloud Run v2 service, Artifact Registry repository, API enablement, IAM bindings, and billing budget. The Neon database is managed outside Terraform since Neon has no official Terraform provider.

Sensitive values (database URL, API keys, tokens) are stored in `terraform.tfvars` which is gitignored and never committed.

## Consequences

### Positive

State is now durable, versioned, and locked. Multiple contributors can safely run Terraform without corrupting state. The GCS bucket versioning provides a recovery path if state gets corrupted. All infrastructure changes are auditable through Terraform plan/apply.

### Negative

Terraform requires GCP authentication (Application Default Credentials or access token) to access the state bucket. The CI/CD pipeline uses Workload Identity Federation, but local development requires `gcloud auth application-default login` or `GOOGLE_OAUTH_ACCESS_TOKEN`. This is a one-time setup per developer.

### Neutral

The GCS bucket itself is not managed by Terraform (bootstrap problem). It was created manually with `gcloud storage buckets create`.

## Alternatives Considered

### Terraform Cloud

HashiCorp's managed state backend with built-in locking, UI, and remote execution.

Why rejected: Adds a third-party service dependency for a single-person project. GCS provides equivalent state storage and locking without an additional account or service.

### Local state with manual backups

Continue with local `.tfstate` file, manually backing up to Git or cloud storage.

Why rejected: No locking, no versioning, error-prone. The GCS backend provides these features natively.

### S3 + DynamoDB (AWS)

The standard Terraform remote backend on AWS.

Why rejected: The project is entirely on GCP. Adding AWS resources for state management would be cross-cloud complexity for no benefit.

## Related

- Terraform config: `terraform/main.tf` (backend block)
- State bucket: `gs://triage-bot-terraform-state`
- CI/CD authentication: `.github/workflows/deploy.yml` (WIF)
