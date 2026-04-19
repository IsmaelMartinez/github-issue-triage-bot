CREATE TABLE IF NOT EXISTS triage_results (
    id              BIGSERIAL PRIMARY KEY,
    repo            TEXT NOT NULL,
    issue_number    INTEGER NOT NULL,
    issue_title     TEXT NOT NULL DEFAULT '',
    draft_comment   TEXT NOT NULL,
    phases_run      TEXT[] NOT NULL DEFAULT '{}',
    phase_details   JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo, issue_number)
);
CREATE INDEX IF NOT EXISTS idx_triage_results_repo ON triage_results (repo);
