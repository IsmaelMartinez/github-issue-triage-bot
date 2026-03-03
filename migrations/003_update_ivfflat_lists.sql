DROP INDEX IF EXISTS idx_issues_embedding;
CREATE INDEX idx_issues_embedding ON issues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 40);
DROP INDEX IF EXISTS idx_documents_embedding;
CREATE INDEX idx_documents_embedding ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 25);
