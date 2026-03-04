-- Agent session state machine: tracks each enhancement issue through the agent pipeline.
CREATE TABLE IF NOT EXISTS agent_sessions (
    id                   BIGSERIAL PRIMARY KEY,
    repo                 TEXT NOT NULL,
    issue_number         INTEGER NOT NULL,
    shadow_repo          TEXT NOT NULL,
    shadow_issue_number  INTEGER,
    stage                TEXT NOT NULL DEFAULT 'new',
    context              JSONB NOT NULL DEFAULT '{}',
    round_trip_count     INTEGER NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (repo, issue_number)
);

-- Audit log: records every agent action for accountability and debugging.
CREATE TABLE IF NOT EXISTS agent_audit_log (
    id                  BIGSERIAL PRIMARY KEY,
    session_id          BIGINT NOT NULL REFERENCES agent_sessions(id),
    action_type         TEXT NOT NULL,
    input_hash          TEXT NOT NULL DEFAULT '',
    output_summary      TEXT NOT NULL DEFAULT '',
    safety_check_passed BOOLEAN NOT NULL DEFAULT true,
    confidence_score    REAL NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_audit_log_session ON agent_audit_log (session_id);

-- Approval gates: tracks pending human approvals before agent proceeds.
CREATE TABLE IF NOT EXISTS approval_gates (
    id          BIGSERIAL PRIMARY KEY,
    session_id  BIGINT NOT NULL REFERENCES agent_sessions(id),
    gate_type   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    approver    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_approval_gates_session ON approval_gates (session_id);
