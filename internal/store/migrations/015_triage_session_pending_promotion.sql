-- Add a column that records when a maintainer's `lgtm` was received but the
-- follow-up promotion to the source repo failed (e.g., cold-start TLS handshake
-- timeout against api.github.com). The daily `/cleanup` cron scans this column
-- and re-attempts the promotion so transient network blips don't silently drop
-- maintainer signals.
ALTER TABLE triage_sessions ADD COLUMN IF NOT EXISTS pending_promotion_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_triage_sessions_pending_promotion
    ON triage_sessions (pending_promotion_at)
    WHERE pending_promotion_at IS NOT NULL AND closed_at IS NULL;
