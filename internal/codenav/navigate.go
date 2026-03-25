package codenav

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
)

const (
	maxFilesToFetch   = 5
	maxFileSize       = 8000
	maxTreePromptSize = 20000
	maxIssueTitleSize = 200
	maxIssueBodySize  = 1500
)

// FileContent holds the path and decoded content of a source file.
type FileContent struct {
	Path    string
	Content string
}

// CodeContext holds fetched source files and can format them for inclusion in an LLM prompt.
type CodeContext struct {
	Files []FileContent
}

// FormatForPrompt returns a markdown-formatted string of all fetched files, suitable for
// inclusion in an LLM prompt. Returns "" if no files are present.
func (cc CodeContext) FormatForPrompt() string {
	if len(cc.Files) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range cc.Files {
		sb.WriteString("### ")
		sb.WriteString(f.Path)
		sb.WriteString("\n```\n")
		sb.WriteString(f.Content)
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}

// Navigator identifies relevant source files for a bug report using the LLM,
// then fetches their contents via the GitHub Contents API.
type Navigator struct {
	github *gh.Client
	llm    llm.Provider
	logger *slog.Logger
}

// New creates a new Navigator.
func New(github *gh.Client, llm llm.Provider, logger *slog.Logger) *Navigator {
	return &Navigator{github: github, llm: llm, logger: logger}
}

// Navigate fetches the repository tree, asks the LLM to identify relevant files,
// validates the paths, and returns a CodeContext with their contents.
func (n *Navigator) Navigate(ctx context.Context, installationID int64, repo, title, body string) (CodeContext, error) {
	entries, err := n.github.GetTree(ctx, installationID, repo, "main")
	if err != nil {
		return CodeContext{}, fmt.Errorf("get tree: %w", err)
	}

	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	filtered := FilterSourceFiles(paths)
	if len(filtered) == 0 {
		n.logger.Info("codenav: no source files found in tree")
		return CodeContext{}, nil
	}

	tree := FormatTreeForLLM(filtered)
	if len(tree) > maxTreePromptSize {
		tree = tree[:maxTreePromptSize] + "\n  ... (truncated)\n"
	}

	systemPrompt := `You are analysing a bug report for a desktop application. Given the bug report and the repository's source file tree, identify the 3-5 most relevant source files that would help diagnose or understand this issue.

Focus on files that:
- Implement the feature or subsystem mentioned in the bug
- Handle configuration, storage, or state relevant to the symptoms
- Contain error handling or logging for the reported behaviour

Return a JSON array of file paths, most relevant first. Only include files from the tree. If no files seem relevant, return [].

Example: ["app/browser/tools/tokenCache.js", "app/config/index.js"]
Respond with ONLY valid JSON, no other text.`

	userContent := fmt.Sprintf("Bug report title: %s\n\nBug report body:\n%s\n\n%s",
		truncateUTF8(title, maxIssueTitleSize), truncateUTF8(body, maxIssueBodySize), tree)

	raw, err := n.llm.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.2, 1024)
	if err != nil {
		return CodeContext{}, fmt.Errorf("identify files: %w", err)
	}

	candidates := parseFileList(raw)
	n.logger.Info("codenav: LLM identified files", "count", len(candidates))

	// Build a set of valid paths for fast lookup.
	validPaths := make(map[string]bool, len(filtered))
	for _, p := range filtered {
		validPaths[p] = true
	}

	var toFetch []string
	for _, p := range candidates {
		if validPaths[p] {
			toFetch = append(toFetch, p)
		}
		if len(toFetch) >= maxFilesToFetch {
			break
		}
	}

	var files []FileContent
	for _, p := range toFetch {
		data, err := n.github.GetFileContents(ctx, installationID, repo, p)
		if err != nil {
			n.logger.Warn("codenav: failed to fetch file", "path", p, "error", err)
			continue
		}
		if data == nil {
			// 404 — skip silently.
			continue
		}
		files = append(files, FileContent{
			Path:    p,
			Content: truncateUTF8(string(data), maxFileSize),
		})
	}

	n.logger.Info("codenav: fetched files", "count", len(files))
	return CodeContext{Files: files}, nil
}

// parseFileList extracts a JSON array of strings from raw LLM output.
// Returns nil if the input cannot be parsed as a JSON array of strings.
func parseFileList(raw string) []string {
	// Find the first balanced JSON array in the response.
	start := strings.IndexByte(raw, '[')
	if start < 0 {
		return nil
	}
	depth := 0
	inString := false
	escaped := false
	end := -1
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '[' {
			depth++
		} else if ch == ']' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil {
		return nil
	}
	return result
}

// truncateUTF8 shortens s to at most maxLen bytes, backing up to a valid UTF-8
// rune boundary so multi-byte sequences are never split.
func truncateUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}
