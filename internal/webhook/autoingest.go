package webhook

import (
	"context"
	"path/filepath"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/ingest"
)

func matchesDocPaths(file string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, file); matched {
			return true
		}
		if matched, _ := filepath.Match(p, filepath.Base(file)); matched {
			return true
		}
		// Handle ** glob by checking if file starts with the prefix before **
		if idx := len(p) - 2; idx > 0 && p[idx:] == "**" {
			prefix := p[:idx]
			if len(file) >= len(prefix) && file[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

func (h *Handler) autoIngestDocs(ctx context.Context, installationID int64, repo string, commits []gh.PushCommit, docPaths []string) {
	seen := make(map[string]bool)
	var toIngest []string

	for _, c := range commits {
		for _, f := range append(c.Added, c.Modified...) {
			if !seen[f] && matchesDocPaths(f, docPaths) {
				seen[f] = true
				toIngest = append(toIngest, f)
			}
		}
	}

	if len(toIngest) == 0 {
		return
	}

	h.logger.Info("auto-ingesting changed docs", "repo", repo, "count", len(toIngest))
	for _, path := range toIngest {
		content, err := h.github.GetFileContents(ctx, installationID, repo, path)
		if err != nil {
			h.logger.Error("fetching doc for auto-ingest", "error", err, "path", path)
			continue
		}
		if content == nil {
			continue
		}
		doc := ingest.DocFromRawContent(repo, path, string(content))
		if err := ingest.EmbedAndUpsert(ctx, h.store, h.llm, doc); err != nil {
			h.logger.Error("auto-ingesting doc", "error", err, "path", path)
		}
	}
}
