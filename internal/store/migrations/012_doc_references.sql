CREATE TABLE doc_references (
    id           BIGSERIAL PRIMARY KEY,
    repo         TEXT NOT NULL,
    source_type  TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    TEXT NOT NULL,
    relationship TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo, source_type, source_id, target_type, target_id, relationship)
);
CREATE INDEX idx_doc_refs_source ON doc_references(repo, source_type, source_id);
CREATE INDEX idx_doc_refs_target ON doc_references(repo, target_type, target_id);
