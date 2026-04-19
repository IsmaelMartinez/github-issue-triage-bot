-- Enable pgvector extension for similarity search
CREATE EXTENSION IF NOT EXISTS vector;

-- Documentation chunks: troubleshooting sections, roadmap items, ADRs, research docs.
-- Each row has an embedding vector for cosine similarity search.
CREATE TABLE IF NOT EXISTS documents (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL DEFAULT 'IsmaelMartinez/teams-for-linux',
    doc_type    TEXT NOT NULL,  -- 'troubleshooting', 'roadmap', 'adr', 'research', 'configuration'
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    embedding   vector(768),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (repo, doc_type, title)
);

-- Issue summaries and embeddings. Updated in real-time via webhook.
-- Replaces issue-index.json.
CREATE TABLE IF NOT EXISTS issues (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT NOT NULL DEFAULT 'IsmaelMartinez/teams-for-linux',
    number      INTEGER NOT NULL,
    title       TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'open',
    labels      TEXT[] NOT NULL DEFAULT '{}',
    milestone   TEXT,
    embedding   vector(768),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ,

    UNIQUE (repo, number)
);

-- Tracks which issues the bot has commented on.
-- Stores reaction counts for accuracy reporting.
CREATE TABLE IF NOT EXISTS bot_comments (
    id              BIGSERIAL PRIMARY KEY,
    repo            TEXT NOT NULL DEFAULT 'IsmaelMartinez/teams-for-linux',
    issue_number    INTEGER NOT NULL,
    comment_id      BIGINT NOT NULL,
    phases_run      TEXT[] NOT NULL DEFAULT '{}',
    thumbs_up       INTEGER NOT NULL DEFAULT 0,
    thumbs_down     INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (repo, issue_number)
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_documents_repo_type ON documents (repo, doc_type);
CREATE INDEX IF NOT EXISTS idx_issues_repo_state ON issues (repo, state);
CREATE INDEX IF NOT EXISTS idx_bot_comments_repo ON bot_comments (repo);

-- ivfflat indexes for vector similarity search (cosine distance)
-- Lists value tuned for small dataset (~500 rows); adjust if dataset grows significantly.
CREATE INDEX IF NOT EXISTS idx_documents_embedding ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 10);
CREATE INDEX IF NOT EXISTS idx_issues_embedding ON issues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 10);
