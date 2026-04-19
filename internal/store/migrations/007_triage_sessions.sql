CREATE TABLE IF NOT EXISTS triage_sessions (
    id                  BIGSERIAL PRIMARY KEY,
    repo                TEXT NOT NULL,
    issue_number        INTEGER NOT NULL,
    shadow_repo         TEXT NOT NULL,
    shadow_issue_number INTEGER NOT NULL,
    triage_comment      TEXT NOT NULL,
    phases_run          TEXT[] NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, issue_number)
);
CREATE INDEX IF NOT EXISTS idx_triage_sessions_shadow ON triage_sessions (shadow_repo, shadow_issue_number);
