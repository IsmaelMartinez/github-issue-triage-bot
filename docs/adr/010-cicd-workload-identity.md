# ADR 010: CI/CD with GitHub Actions and Workload Identity Federation

## Status

Implemented

## Context

The bot needs automated build and deploy on push to main. The traditional approach for GitHub Actions to GCP authentication uses long-lived service account keys stored as GitHub secrets. These keys are a security risk: they do not expire by default, a leak grants full service account permissions until manually rotated, and they are difficult to audit since key usage is not tied to specific workflow runs.

## Decision

Use Workload Identity Federation (WIF) for keyless authentication from GitHub Actions to GCP. The deploy workflow authenticates via OIDC token exchange — the GitHub Actions runner requests an OIDC token from GitHub's token endpoint, presents it to GCP's Security Token Service, and receives a short-lived federated access token scoped to the deploy service account. No service account keys exist anywhere.

The WIF pool and provider are scoped to the specific GitHub repository (`IsmaelMartinez/github-issue-triage-bot`) via attribute conditions, preventing other repositories from impersonating the service account.

All third-party GitHub Actions used in the pipeline are pinned to specific commit SHAs rather than version tags. This mitigates supply chain attacks where a compromised action could exfiltrate secrets or inject malicious code — a tag can be moved to point to different code, but a commit SHA is immutable.

The pipeline handles building and pushing container images to Artifact Registry and updating the Cloud Run service. It does not manage secrets; those are handled separately via Terraform (see ADR 007).

## Consequences

### Positive

No service account keys to rotate, store, or risk leaking. Federated tokens are short-lived (one hour by default) and scoped to the specific workflow run. SHA-pinned actions provide an immutable supply chain. The WIF attribute condition ensures only this repository can authenticate as the deploy service account.

### Negative

SHA-pinned actions require manual updates when upgrading to new versions — there is no automatic notification when a pinned action releases a security fix. The WIF pool and provider configuration in Terraform is verbose and must be correctly scoped. Initial setup is more complex than dropping a service account key into GitHub secrets.

### Neutral

The workflow structure (checkout, authenticate, build, push, deploy) is the same regardless of authentication method. Switching from key-based to WIF-based authentication does not change what the pipeline does, only how it authenticates.

## Related

- Deploy workflow: `.github/workflows/deploy.yml`
- WIF pool: `projects/62054333602/locations/global/workloadIdentityPools/github-actions`
- Deploy service account: `triage-bot-deploy@gen-lang-client-0421325030.iam.gserviceaccount.com`
- Terraform WIF config: `terraform/main.tf`
