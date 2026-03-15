package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

const (
	// syncTimeout is the maximum time allowed for a mirror sync operation.
	syncTimeout = 10 * time.Minute
)

// Service manages bare-clone mirrors of public repos into private shadow repos.
type Service struct {
	github   *gh.Client
	logger   *slog.Logger
	cacheDir string

	// mu protects against concurrent syncs of the same source repo.
	mu sync.Mutex
}

// New creates a new mirror Service. cacheDir is the directory where bare clones are cached.
func New(github *gh.Client, logger *slog.Logger, cacheDir string) *Service {
	return &Service{
		github:   github,
		logger:   logger,
		cacheDir: cacheDir,
	}
}

// Sync mirrors a source repo to its shadow repo. It performs a bare clone (or fetch if
// already cached) and pushes all refs to the shadow repo using an installation token.
func (s *Service) Sync(ctx context.Context, installationID int64, sourceRepo, shadowRepo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	log := s.logger.With("sourceRepo", sourceRepo, "shadowRepo", shadowRepo)
	log.Info("starting mirror sync")

	token, err := s.github.InstallationToken(ctx, installationID)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	// Sanitised directory name from repo slug
	safeName := strings.ReplaceAll(sourceRepo, "/", "--")
	bareDir := filepath.Join(s.cacheDir, safeName+".git")

	sourceURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, sourceRepo)
	shadowURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, shadowRepo)

	if _, err := os.Stat(bareDir); os.IsNotExist(err) {
		// Initial bare clone
		log.Info("performing initial bare clone")
		if err := s.runGit(ctx, "", "clone", "--bare", sourceURL, bareDir); err != nil {
			// Clean up partial clone on failure
			_ = os.RemoveAll(bareDir)
			return fmt.Errorf("bare clone: %w", err)
		}
	} else {
		// Fetch updates into existing bare clone
		log.Info("fetching updates into existing bare clone")
		if err := s.runGit(ctx, bareDir, "fetch", "--prune", sourceURL, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
			return fmt.Errorf("fetch updates: %w", err)
		}
	}

	// Push mirror to shadow repo
	log.Info("pushing mirror to shadow repo")
	if err := s.runGit(ctx, bareDir, "push", "--mirror", shadowURL); err != nil {
		return fmt.Errorf("push mirror: %w", err)
	}

	log.Info("mirror sync complete")
	return nil
}

// runGit executes a git command, redacting tokens from error output.
func (s *Service) runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Prevent git from prompting for credentials
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Redact any tokens from error output
		sanitized := redactTokens(string(output))
		return fmt.Errorf("git %s: %s: %w", args[0], sanitized, err)
	}
	return nil
}

// redactTokens replaces x-access-token credentials in a string.
func redactTokens(s string) string {
	const prefix = "x-access-token:"
	const redacted = "x-access-token:***"
	var result strings.Builder
	for {
		start := strings.Index(s, prefix)
		if start == -1 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:start])
		rest := s[start+len(prefix):]
		at := strings.Index(rest, "@")
		if at == -1 {
			result.WriteString(s[start:])
			break
		}
		result.WriteString(redacted)
		s = rest[at:]
	}
	return result.String()
}
