# ADR 009: Webhook Replay Protection via Delivery ID Tracking

## Status

Implemented

## Context

GitHub retries webhook deliveries when the receiving server returns an error or times out. Network issues between GitHub and Cloud Run can also cause duplicate deliveries. Without deduplication, the bot could post multiple identical triage comments on the same issue, creating noise for maintainers and making the bot appear broken.

GitHub assigns a unique delivery ID to each webhook event via the `X-GitHub-Delivery` header. This ID is stable across retries of the same logical delivery but distinct for separate events.

## Decision

Track webhook delivery IDs in a `webhook_deliveries` table using an atomic INSERT ON CONFLICT pattern implemented as a CTE. When a webhook arrives, the handler checks the delivery ID immediately after signature verification. If the ID already exists in the table, the handler returns 200 (already processed) and skips triage. If the ID is new, it is inserted atomically and processing continues.

The CTE pattern ensures that concurrent deliveries of the same ID are handled correctly — only one will succeed in inserting, and the others will see the conflict and return early.

Entries older than 30 days are cleaned up periodically to bound table growth. The 30-day window is well beyond GitHub's retry window (which is typically hours, not days), providing a generous safety margin.

## Consequences

### Positive

Guarantees at-most-once processing per delivery ID, preventing duplicate comments. The atomic CTE pattern handles concurrent duplicate deliveries safely without requiring application-level locking. The 30-day TTL keeps the table small.

### Negative

Each webhook request incurs one additional database write (the delivery ID insert). If the database is unavailable, the handler returns 500, causing GitHub to retry later — this is the correct behavior since we cannot guarantee idempotency without the database. The periodic cleanup requires either a cron job or in-process scheduler.

### Neutral

The delivery ID table is append-only during normal operation. Reads only happen during the INSERT ON CONFLICT check, so there is no contention with other queries. The table has a unique index on the delivery ID column.

## Related

- Webhook handler: `internal/webhook/handler.go`
- Database migration: `migrations/002_webhook_deliveries.sql`
- Store implementation: `internal/store/postgres.go`
