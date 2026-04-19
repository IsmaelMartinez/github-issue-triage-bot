CREATE TABLE IF NOT EXISTS feedback_signals (
    id              BIGSERIAL PRIMARY KEY,
    repo            TEXT NOT NULL,
    issue_number    INTEGER NOT NULL,
    signal_type     TEXT NOT NULL,
    details         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_signals_repo
    ON feedback_signals (repo);
CREATE INDEX IF NOT EXISTS idx_feedback_signals_repo_issue
    ON feedback_signals (repo, issue_number);
CREATE INDEX IF NOT EXISTS idx_feedback_signals_type
    ON feedback_signals (signal_type);
