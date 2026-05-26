package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func cleanupOldVersions(ctx context.Context, s *store.Store, repo string, logger *slog.Logger) error {
	activeStr := os.Getenv("ACTIVE_VERSIONS")
	if activeStr == "" {
		return fmt.Errorf("ACTIVE_VERSIONS env var required (e.g. '40,41,42')")
	}
	var activeVersions []int
	for _, v := range strings.Split(activeStr, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("parse version %q: %w", v, err)
		}
		activeVersions = append(activeVersions, n)
	}

	maxActive := 0
	for _, v := range activeVersions {
		if v > maxActive {
			maxActive = v
		}
	}
	cutoff := maxActive - 2

	for v := cutoff; v >= 0; v-- {
		ver := strconv.Itoa(v)
		n, err := s.DeleteDocumentsByVersion(ctx, repo, store.UpstreamDocTypes, ver)
		if err != nil {
			logger.Error("cleanup failed", "version", ver, "error", err)
			continue
		}
		if n > 0 {
			logger.Info("cleaned up old version", "version", ver, "deleted", n)
		}
	}
	return nil
}
