# Runbook: executing the retirement of the triage-bot service

Status: Proposed (documentation only; nothing below has been executed)
Date: 2026-11-06
Implements: `docs/decisions/009-retire-the-service.md` and `docs/plans/2026-06-05-retire-service-keep-capability.md`

## Why this document exists

ADR-009 decided to retire the service and the June plan described the steps, but execution was left unscheduled. This runbook adds what those documents could not contain on the day they were written: an inventory of what is verifiably still running on 2026-11-06, the prerequisites that are still unmet, and the order of the destructive steps with the exact commands, so that each step can be approved on its own. It changes no decision. Where it differs from the June plan it says so.

## What is running today

The Cloud Run service `triage-bot` in project `gen-lang-client-0421325030` (us-central1) answers at `https://triage-bot-lhuutxzbnq-uc.a.run.app`; `/health` returned `ok` on this date. It reads and writes a Neon PostgreSQL 17 database with pgvector (project `falling-resonance-06310725`, aws-us-east-2) and calls Gemini for embeddings. Terraform in `terraform/main.tf` owns the Artifact Registry repository, the runtime service account, five Secret Manager secrets, the Cloud Run service with its public invoker binding, the billing budget, and five `google_project_service` blocks; its state is in `gs://triage-bot-terraform-state`. The deploy service account `triage-bot-deploy` is reached from GitHub Actions through the Workload Identity pool `github-actions`, which is not in the Terraform file and therefore survives a destroy.

The GitHub App (the bot account is `@ismael-triage-bot`) delivers issue webhooks from teams-for-linux. The repository webhook list for teams-for-linux contains only Codebeat and Snyk hooks, which is consistent with an App-level webhook; the installation itself cannot be listed with a user token (the installations endpoint answers 401), so the installation must be confirmed in the GitHub UI under Settings, Applications, Installed GitHub Apps.

Scheduled workflows in this repository that still run and that touch the service or its database are Daily Maintenance (06:00 cron, workflow `dashboard.yml`), Daily Event Ingest, and Upstream watcher, all successful on 2026-11-06. Also defined are Weekly Synthesis, Weekly Webhook Catch-Up, Monthly Upstream Refresh, the manual Seed Database, the twice-monthly Scheduled release, and the generic CodeQL, OSV-Scanner and Dependabot workflows, which do not depend on the service. The CI/CD workflow (`deploy.yml`) deploys on pushes to main; its most recent pull-request run, on 2026-11-04, failed and should be understood before any teardown relies on it. Repository Actions secrets are `DATABASE_URL`, `TF_VAR_DATABASE_URL`, `TF_VAR_GEMINI_API_KEY`, `TF_VAR_GITHUB_APP_ID`, `TF_VAR_GITHUB_PRIVATE_KEY`, `TF_VAR_WEBHOOK_SECRET` and `TF_VAR_BILLING_ACCOUNT_ID`. A GitHub Pages site exists at `https://ismaelmartinez.github.io/github-issue-triage-bot/`.

Consumers outside this repository are three. The Claude Code user configuration (`~/.claude-home/.claude.json`) registers an MCP server `triage-bot` that runs the local binary `bin/triage-mcp` against the Cloud Run URL. The private skill `teams-for-linux-issue-review` (`~/.claude-home/skills/teams-for-linux-issue-review/SKILL.md`) calls `mcp__triage-bot__get_brief_preview`, falls back to a `curl` on `/brief-preview`, and posts maintainer corrections to `/learning`. In teams-for-linux, `.github/hats.md` is documented as input to the bot's retrieval, and ADR-018, the roadmap and the research index link to this repository; none of those is a runtime dependency, and no teams-for-linux workflow calls the service (searched for `triage-bot` and `run.app` across the repository).

## Prerequisites that are not yet met

The June plan makes decommissioning conditional on the replacement being proven. As of today teams-for-linux contains no `.claude/skills` directory (only `settings.json` and `settings.local.json` under `.claude/`), no committed embedding index and no refresh Action, and the skill still lives in the private home directory pointing at Cloud Run. So the replacement has not been built, and phases 1 and 2 below must happen before any teardown. Two decisions the plan left open also need an answer first: how the changing index is stored (the plan leans towards commit-on-change) and whether the service is destroyed straight after the bridge is proven or left scaled to zero for a grace period. This runbook recommends the grace period.

## Phased plan

Phase 0 is this documentation change, plus confirming in the GitHub UI whether the App installation covers only teams-for-linux. It is reversible by reverting the pull request.

Phase 1 builds the replacement in teams-for-linux, in a separate pull request that needs its own approval: the scrubbed skill, a pinned CPU embedder, the committed index and the refresh Action, as specified in steps 1 to 5 of the June plan. Nothing in this repository changes. Success is the skill answering real issues from the local index while the known auth and login neighbours (#1267, #448, #648) still surface.

Phase 2 is a trial period of a few weeks in which the skill uses the local index and the service stays up. Rollback is reverting the single skill edit. During this time, optionally stop the scheduled workflows that only feed the dashboard and synthesis, which nobody consumes, by disabling them rather than deleting them (commands below, all reversible).

Phase 3 switches off the inbound path: remove the App's access to teams-for-linux, then scale Cloud Run to zero. These are the first steps that change live behaviour, and both are reversible within the grace period because the App can be reinstalled and the revision scaled back.

Phase 4, after the grace period with no incident, is destructive and irreversible: destroy the Terraform infrastructure, delete the Neon project, remove the secrets, drop the local MCP registration, and archive the repository. What to keep: the Git history is the record of the experiment; the issue corpus is public and is regenerated from the GitHub API, so the Neon data is not migrated; if any private data is wanted, take a `pg_dump` before deleting the Neon project.

## Steps awaiting approval, in order

Each command below is listed for later approval and has not been run. Run them one at a time.

Step 1 (reversible), disable the scheduled workflows that feed only the unused dashboard and synthesis:

    for w in dashboard.yml event-ingest.yml synthesis.yml upstream-watcher.yml upstream-refresh.yml webhook-catchup.yml; do gh workflow disable $w -R IsmaelMartinez/github-issue-triage-bot; done

Re-enable with `gh workflow enable`. Do this only after phase 2 has run long enough to be sure nothing reads the output; the Daily Maintenance run also performs cleanup and health checks the service relies on while it is live.

Step 2 (reversible within the grace period), uninstall the App from teams-for-linux. This is a UI action: github.com, Settings, Applications, Installed GitHub Apps, the triage bot, Configure, then either remove the teams-for-linux repository from the selection or Uninstall. There is no supported `gh` command for it with a user token.

Step 3 (reversible), scale the service to zero rather than deleting it:

    gcloud run services update triage-bot --region us-central1 --project gen-lang-client-0421325030 --min-instances 0 --max-instances 0

Terraform will fight this setting on the next deploy, so also disable `deploy.yml` first with `gh workflow disable deploy.yml -R IsmaelMartinez/github-issue-triage-bot`.

Step 4 (reversible), remove the local MCP registration and update the skill to the local index. For the registration: `claude mcp remove triage-bot -s user`. Do this for the personal profile only; the work profile has no registration (checked in `~/.claude.json`, which does not contain it).

Step 5 (destructive), confirm the GCP project is not shared, because destroy disables the five APIs project-wide; if it is shared, remove the `google_project_service` resources from state first with `terraform state rm` for each. Then from `terraform/`:

    terraform destroy

The Workload Identity pool and the state bucket are outside Terraform and stay until step 7.

Step 6 (destructive, irreversible), delete the Neon project `falling-resonance-06310725` after an optional dump:

    pg_dump "$DATABASE_URL" -Fc -f triage-bot-final.dump
    neonctl projects delete falling-resonance-06310725

Step 7 (destructive), delete the leftovers that Terraform does not own: the Workload Identity pool and provider, the `triage-bot-deploy` service account, and the state bucket `gs://triage-bot-terraform-state`, then delete the repository Actions secrets (`gh secret delete NAME -R IsmaelMartinez/github-issue-triage-bot` for each of the seven listed above) and delete the GitHub App itself in its developer settings, which also invalidates its private key.

Step 8 (reversible by unarchiving), archive the repository so Actions stops wholesale and the history stays readable:

    gh repo archive IsmaelMartinez/github-issue-triage-bot --yes

Last, in teams-for-linux, a small documentation pull request should mark ADR-018 as superseded and drop the hats.md reference to the bot. That is outside this change and is not prepared here.

## What replaces each function

Semantic recall over issue history moves to the committed index in teams-for-linux, with GitHub's native semantic issue search as the eventual backend. Reply drafting and hat classification already live in the skill. Documentation lookup is done by the agent reading `docs/`. The dashboard, weekly synthesis, health monitor, trend endpoint, upstream candidates and learnings were measured as unconsumed in ADR-009 and have no replacement.
