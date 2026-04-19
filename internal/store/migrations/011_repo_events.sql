CREATE TABLE IF NOT EXISTS repo_events (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    source_ref  TEXT,
    summary     TEXT NOT NULL,
    areas       TEXT[],
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_repo_events_repo_type ON repo_events(repo, event_type);
CREATE INDEX IF NOT EXISTS idx_repo_events_repo_created ON repo_events(repo, created_at DESC);
