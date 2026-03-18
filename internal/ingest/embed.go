package ingest

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// DocFromRawContent creates a Document from a file path and its raw markdown content.
// The doc_type is inferred from the file path.
func DocFromRawContent(repo, path, content string) store.Document {
	return store.Document{
		Repo:    repo,
		DocType: inferDocType(path),
		Title:   path,
		Content: content,
		Metadata: map[string]any{
			"doc_path": path,
		},
	}
}

// EmbedAndUpsert embeds a document and upserts it into the store.
func EmbedAndUpsert(ctx context.Context, s *store.Store, l llm.Provider, doc store.Document) error {
	text := fmt.Sprintf("%s\n%s", doc.Title, doc.Content)
	if len(text) > 2000 {
		cut := 2000
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	embedding, err := l.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed %q: %w", doc.Title, err)
	}
	doc.Embedding = embedding
	if err := s.UpsertDocument(ctx, doc); err != nil {
		return fmt.Errorf("upsert %q: %w", doc.Title, err)
	}

	// Extract and record cross-references (best-effort — don't fail the upsert)
	refs := store.ExtractReferences(doc.Content)
	if len(refs) > 0 {
		if refErr := s.RecordReferences(ctx, doc.Repo, "document", doc.Title, refs); refErr != nil {
			return fmt.Errorf("record references for %q: %w", doc.Title, refErr)
		}
	}
	return nil
}

func inferDocType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "adr"):
		return "adr"
	case strings.Contains(lower, "research"):
		return "research"
	case strings.Contains(lower, "roadmap") || strings.Contains(lower, "plan"):
		return "roadmap"
	case strings.Contains(lower, "troubleshoot"):
		return "troubleshooting"
	default:
		return "configuration"
	}
}
