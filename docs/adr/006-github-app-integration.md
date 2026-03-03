# ADR 006: GitHub App for Repository Integration

## Status

Implemented

## Context

The triage bot currently integrates with GitHub via a plain webhook configured on each repository, authenticated with a personal access token (PAT) for posting comments. This approach has several drawbacks: the PAT is tied to a specific user account, webhook configuration is manual per-repo, and there is no formal permission model beyond "this token can do everything the user can do."

As we prepare to cut over from the test repository to teams-for-linux (and potentially other repositories in the future), we need a more robust integration mechanism that handles authentication properly, is easy for repo admins to install, and presents a clear identity when posting comments.

## Decision

Register a GitHub App that serves as the formal integration point between repositories and the triage bot service. The Cloud Run service becomes the app's webhook backend.

The app requests minimal permissions: Issues (read/write) for posting triage comments, and subscribes to the `issues` event. When a repository installs the app, GitHub routes issue events to the service's webhook URL. The service authenticates API calls using installation access tokens (short-lived, scoped to the installing repo) rather than a PAT.

Authentication flow: the service holds the app's private key and generates a JWT signed with RS256. For each webhook delivery, it exchanges the JWT for an installation access token by calling `POST /app/installations/{installation_id}/access_tokens`. The `installation_id` comes from the webhook payload. The Go library `bradleyfalzon/ghinstallation` handles this token lifecycle transparently.

The private key is stored as a Cloud Run secret (via Secret Manager) rather than in environment variables.

## Consequences

### Positive

The app authenticates as itself, not as a user, eliminating dependency on a personal access token. Installation is one-click for any repository admin — no secrets to configure, no workflow files to add. The permission model is granular (only Issues read/write, nothing else). Rate limits are higher for GitHub Apps (5,000 requests/hour per installation). The bot gets its own identity with avatar and name in comments. The app can be installed on multiple repos without duplicating configuration.

### Negative

More complex authentication flow compared to a PAT. The JWT/installation token dance requires a private key, which must be stored securely and rotated if compromised. The `bradleyfalzon/ghinstallation` library adds a dependency. Local development requires webhook tunneling (smee.io or ngrok) unless testing against the deployed service.

### Neutral

The webhook payload format is identical whether delivered to a plain webhook or a GitHub App. The existing webhook handler code needs minimal changes — only the authentication layer changes from PAT to installation tokens.

## Alternatives Considered

### Plain webhook with PAT (current approach)

Continue using manual webhook configuration and a personal access token for API calls.

Why rejected: PATs are tied to user accounts, have broad permissions, and require manual rotation. Webhook configuration is manual per-repo. Not suitable for a multi-repo service.

### Reusable GitHub Action

Publish a GitHub Action that consuming repos call from a workflow triggered on issue events. The action would either contain the triage logic or call the Cloud Run service.

Why rejected: This fundamentally changes the architecture. The triage logic depends on Cloud Run (for Gemini API calls and database access), so the action would just be a thin HTTP wrapper adding ceremony without value. Each consuming repo needs to add a workflow file and configure secrets (Gemini API key, database URL). Defeats the purpose of a centralized service.

### GitHub CLI extension

A `gh` CLI extension for interactive or scripted triage.

Why rejected: CLI extensions are for interactive terminal use. They have no webhook integration and cannot receive events. Not applicable to automated, event-driven triage.

## Related

- Current webhook handler: `internal/webhook/handler.go`
- Current GitHub client (PAT-based): `internal/github/client.go`
- Cloud Run service: `terraform/main.tf`
