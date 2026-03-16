# Phase F1: Feedback Tracking

Date: 2026-03-16
Status: Future (not started)

## Summary

Track whether the bot's output actually helps users. The strongest signal is whether users edit their issue to fill in sections that Phase 1 flagged as missing. The `issues.edited` webhook event already arrives at the bot but is ignored — the payload includes the old body for free.

## Spike Results

- `issues.edited` webhook includes `changes.body.from` with the old body (zero API calls)
- GraphQL `userContentEdits` can retroactively detect edits (requires new client)
- Bot currently ignores `issues.edited` in handler.go action switch
- No `feedback_signals` table exists; latest migration is 009
- Issue body is not stored at triage time (need snapshot column)

## Implementation Sketch

Migration 010: `feedback_signals` table (repo, issue_number, signal_type, details JSONB). Migration 011: add `issue_body_snapshot TEXT` to `triage_sessions`. Handle `issues.edited` in webhook handler — compare old vs new body against Phase 1 results. Store feedback signals. Surface Phase 1 fill rate on dashboard.

See full implementation details in the session where this was planned (2026-03-16).

## Trigger to start

When we have enough triage data (20+ promoted sessions) to make the metrics meaningful, or when preparing for Stage A → B transition.
