CREATE TABLE IF NOT EXISTS bot_settings (
    repo TEXT PRIMARY KEY,
    paused BOOLEAN NOT NULL DEFAULT FALSE,
    paused_at TIMESTAMPTZ,
    paused_by TEXT
);
