# Code Navigation for Triage Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the triage bot on-demand access to the target repo's source code via the GitHub API, so Phase 2 and enhancement research can reference actual code paths instead of only documentation.

**Architecture:** Three-step approach: (1) fetch the repo's file tree via the Git Trees API in a single call, (2) ask the LLM to identify relevant source files from the tree based on the issue content, (3) fetch those files via the Contents API and include them as additional context in the Phase 2 prompt. No pre-indexing, no embeddings, no storage — all on-demand per triage. Phase 4a and agent research are out of scope for this iteration; code context is injected into Phase 2 only.

**Tech Stack:** Go 1.26, GitHub Git Trees API, GitHub Contents API, existing LLM client (Gemini 2.5 Flash)

**Cost:** 1 tree API call + 1 LLM call (file identification, counts against daily limit) + 2-5 Contents API calls per triage. Roughly 50% increase in LLM calls per triage vs current. All within GitHub API rate limits and Gemini free tier.

**Scope:** Phase 2 only in this iteration. Phase 4a and agent research may be extended later once code navigation quality is validated. Partially addresses issue #50 (code-aware triage) via on-demand API access rather than repository mirroring.

**Limitations:** Default branch hardcoded to "main" (matches existing codebase convention). Git Trees API may return truncated results for repos with >100K files (logged as warning, not expected for current targets).

---

## File Structure

```
internal/codenav/tree.go          # Source file filtering and tree formatting
internal/codenav/tree_test.go     # Tests for filtering and formatting
internal/codenav/navigate.go      # LLM-based file identification + content fetching
internal/codenav/navigate_test.go # Tests for navigation logic
internal/github/client.go         # Add GetTree method (alongside existing GetFileContents)
internal/phases/phase2.go         # Accept optional code context in prompt
internal/phases/phase2_test.go    # Update callers for new signature
internal/webhook/handler.go       # Wire up code navigation before phases
internal/config/butler.go         # Add code_navigation capability flag
cmd/backfill/main.go              # Update Phase2 caller for new signature
```

---

## Task 1: Add GetTree to GitHub Client

**Files:**
- Modify: `internal/github/client.go`
- Modify: `internal/github/client_test.go`

Add a method to fetch the full file tree via `GET /repos/{owner}/{repo}/git/trees/{sha}?recursive=1`.

- [ ] **Step 1: Write the failing test**

```go
// internal/github/client_test.go
func TestGetTree(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.Contains(r.URL.Path, "/git/trees/") {
            json.NewEncoder(w).Encode(map[string]any{
                "sha": "abc123",
                "tree": []map[string]string{
                    {"path": "app/index.js", "type": "blob"},
                    {"path": "app/config", "type": "tree"},
                    {"path": "app/config/index.js", "type": "blob"},
                },
            })
            return
        }
        // ... existing token endpoint handling
    }))
    // ...
    entries, err := client.GetTree(ctx, installationID, "owner/repo", "main")
    if err != nil { t.Fatal(err) }
    if len(entries) != 2 { // only blobs, not trees
        t.Fatalf("expected 2 blob entries, got %d", len(entries))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -run TestGetTree -v`
Expected: FAIL — `GetTree` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// TreeEntry represents a file in the repo tree.
type TreeEntry struct {
    Path string `json:"path"`
    Type string `json:"type"` // "blob" or "tree"
    Size int    `json:"size"`
}

// GetTree fetches the full recursive file tree for a branch.
// Returns only blob entries (files, not directories).
func (c *Client) GetTree(ctx context.Context, installationID int64, repo, branch string) ([]TreeEntry, error) {
    client, err := c.installationClient(installationID)
    if err != nil {
        return nil, fmt.Errorf("installation client: %w", err)
    }
    url := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.baseURL, repo, branch)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("Accept", "application/vnd.github+json")
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("send request: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
    }
    var result struct {
        Tree      []TreeEntry `json:"tree"`
        Truncated bool        `json:"truncated"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }
    if result.Truncated {
        // Log warning but continue with partial tree — large repos may exceed API limits
        fmt.Fprintf(os.Stderr, "warning: tree for %s/%s is truncated, code navigation may be incomplete\n", repo, branch)
    }
    // Filter to blobs only
    blobs := make([]TreeEntry, 0, len(result.Tree))
    for _, e := range result.Tree {
        if e.Type == "blob" {
            blobs = append(blobs, e)
        }
    }
    return blobs, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -run TestGetTree -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "feat: add GetTree method to GitHub client"
```

---

## Task 2: Code Navigation Package — Tree Cache + Source File Filter

**Files:**
- Create: `internal/codenav/tree.go`
- Create: `internal/codenav/tree_test.go`

Fetch and cache the file tree, filter to source files only (exclude assets, tests, vendored code, lock files).

- [ ] **Step 1: Write the failing test**

```go
// internal/codenav/tree_test.go
package codenav

import (
    "strings"
    "testing"
)

func TestFilterSourceFiles(t *testing.T) {
    entries := []string{
        "app/index.js",
        "app/config/index.js",
        "app/assets/icon.png",
        "package-lock.json",
        "node_modules/foo/index.js",
        "tests/unit/foo.test.js",
        ".github/workflows/build.yml",
        "docs-site/docs/adr/001.md",
        "app/browser/tools/tokenCache.js",
    }
    filtered := FilterSourceFiles(entries)
    // Should keep app/ JS files, exclude assets, lock files, node_modules, tests, .github, docs-site
    expected := []string{
        "app/index.js",
        "app/config/index.js",
        "app/browser/tools/tokenCache.js",
    }
    if len(filtered) != len(expected) {
        t.Fatalf("expected %d files, got %d: %v", len(expected), len(filtered), filtered)
    }
}

func TestFormatTreeForLLM(t *testing.T) {
    paths := []string{
        "app/index.js",
        "app/config/index.js",
        "app/browser/tools/tokenCache.js",
    }
    result := FormatTreeForLLM(paths)
    if result == "" {
        t.Fatal("expected non-empty tree string")
    }
    if !strings.Contains(result, "tokenCache.js") {
        t.Error("expected tokenCache.js in output")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codenav/ -run TestFilter -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/codenav/tree.go
package codenav

import (
    "path/filepath"
    "strings"
)

// Source file extensions worth including in code navigation.
var sourceExtensions = map[string]bool{
    ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
    ".go": true, ".py": true, ".rb": true, ".rs": true,
    ".java": true, ".kt": true, ".swift": true,
    ".c": true, ".h": true, ".cpp": true,
    ".sh": true, ".bash": true,
    ".json": true, ".yml": true, ".yaml": true, ".toml": true,
}

// Directories to exclude from code navigation.
var excludeDirs = []string{
    "node_modules/", "vendor/", ".git/", "dist/", "build/",
    "assets/", "fonts/", "icons/",
    "tests/", "test/", "__tests__/", "spec/",
    ".github/", "docs-site/", "docs/",
}

// Filenames to exclude.
var excludeFiles = map[string]bool{
    "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
    "go.sum": true, "Cargo.lock": true,
}

// FilterSourceFiles returns only source code paths from a full file list,
// excluding assets, tests, vendored code, lock files, and docs.
func FilterSourceFiles(paths []string) []string {
    var result []string
    for _, p := range paths {
        if excludeFiles[filepath.Base(p)] {
            continue
        }
        ext := filepath.Ext(p)
        if !sourceExtensions[ext] {
            continue
        }
        excluded := false
        for _, dir := range excludeDirs {
            if strings.Contains(p, dir) {
                excluded = true
                break
            }
        }
        if excluded {
            continue
        }
        result = append(result, p)
    }
    return result
}

// FormatTreeForLLM formats a list of file paths as a compact tree string
// suitable for inclusion in an LLM prompt.
func FormatTreeForLLM(paths []string) string {
    if len(paths) == 0 {
        return ""
    }
    var sb strings.Builder
    sb.WriteString("Source files:\n")
    for _, p := range paths {
        sb.WriteString("  ")
        sb.WriteString(p)
        sb.WriteByte('\n')
    }
    return sb.String()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/codenav/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/codenav/
git commit -m "feat: add source file filtering and tree formatting for code navigation"
```

---

## Task 3: Code Navigation — LLM File Identification + Content Fetching

**Files:**
- Create: `internal/codenav/navigate.go`
- Create: `internal/codenav/navigate_test.go`

Given an issue title+body and the file tree, ask the LLM which files are most relevant, then fetch their contents via the GitHub Contents API.

- [ ] **Step 1: Write the failing test**

```go
// internal/codenav/navigate_test.go
func TestParseFileList(t *testing.T) {
    raw := `["app/browser/tools/tokenCache.js", "app/config/index.js"]`
    files := parseFileList(raw)
    if len(files) != 2 {
        t.Fatalf("expected 2 files, got %d", len(files))
    }
}

func TestParseFileList_InvalidJSON(t *testing.T) {
    files := parseFileList("not json")
    if len(files) != 0 {
        t.Fatalf("expected 0 files for invalid JSON, got %d", len(files))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codenav/ -run TestParseFileList -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/codenav/navigate.go
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
    "github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
)

const maxFilesToFetch = 5
const maxFileSize = 8000 // bytes per file to include in prompt

const identifyFilesPrompt = `You are analysing a bug report for a desktop application. Given the bug report and the repository's source file tree, identify the 3-5 most relevant source files that would help diagnose or understand this issue.

Focus on files that:
- Implement the feature or subsystem mentioned in the bug
- Handle configuration, storage, or state relevant to the symptoms
- Contain error handling or logging for the reported behaviour

Return a JSON array of file paths, most relevant first. Only include files from the tree. If no files seem relevant, return [].

Example: ["app/browser/tools/tokenCache.js", "app/config/index.js"]
Respond with ONLY valid JSON, no other text.`

// Navigator provides on-demand code navigation for triage.
type Navigator struct {
    github *gh.Client
    llm    llm.Provider
    logger *slog.Logger
}

// New creates a Navigator.
func New(github *gh.Client, llm llm.Provider, logger *slog.Logger) *Navigator {
    return &Navigator{github: github, llm: llm, logger: logger}
}

// CodeContext holds fetched source file contents for inclusion in LLM prompts.
type CodeContext struct {
    Files []FileContent
}

// FileContent holds a single file's path and truncated content.
type FileContent struct {
    Path    string
    Content string
}

// FormatForPrompt returns the code context as a string for LLM prompt injection.
func (cc CodeContext) FormatForPrompt() string {
    if len(cc.Files) == 0 {
        return ""
    }
    var sb strings.Builder
    sb.WriteString("\nRelevant source code from the repository:\n\n")
    for _, f := range cc.Files {
        sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", f.Path, f.Content))
    }
    return sb.String()
}

// Navigate fetches the file tree, identifies relevant files via LLM, and
// returns their contents as code context for triage prompts.
func (n *Navigator) Navigate(ctx context.Context, installationID int64, repo, title, body string) (CodeContext, error) {
    n.logger.Info("codenav start", "repo", repo)

    // Step 1: Fetch file tree
    entries, err := n.github.GetTree(ctx, installationID, repo, "main")
    if err != nil {
        return CodeContext{}, fmt.Errorf("get tree: %w", err)
    }
    paths := make([]string, len(entries))
    for i, e := range entries {
        paths[i] = e.Path
    }
    sourcePaths := FilterSourceFiles(paths)
    n.logger.Info("codenav tree fetched", "total", len(entries), "source", len(sourcePaths))

    if len(sourcePaths) == 0 {
        return CodeContext{}, nil
    }

    // Step 2: Ask LLM which files are relevant
    tree := FormatTreeForLLM(sourcePaths)
    userContent := fmt.Sprintf("%s\n\nBug report:\nTitle: %s\nBody: %s",
        tree, truncateUTF8(title, 200), truncateUTF8(body, 1500))

    raw, err := n.llm.GenerateJSONWithSystem(ctx, identifyFilesPrompt, userContent, 0.2, 1024)
    if err != nil {
        return CodeContext{}, fmt.Errorf("identify files: %w", err)
    }
    filePaths := parseFileList(raw)
    n.logger.Info("codenav files identified", "files", filePaths)

    if len(filePaths) == 0 {
        return CodeContext{}, nil
    }
    if len(filePaths) > maxFilesToFetch {
        filePaths = filePaths[:maxFilesToFetch]
    }

    // Validate paths exist in tree
    pathSet := make(map[string]bool, len(sourcePaths))
    for _, p := range sourcePaths {
        pathSet[p] = true
    }
    var validPaths []string
    for _, p := range filePaths {
        if pathSet[p] {
            validPaths = append(validPaths, p)
        }
    }

    // Step 3: Fetch file contents
    var files []FileContent
    for _, p := range validPaths {
        content, err := n.github.GetFileContents(ctx, installationID, repo, p)
        if err != nil {
            n.logger.Error("codenav fetch file", "path", p, "error", err)
            continue
        }
        if content == nil {
            continue
        }
        text := string(content)
        if len(text) > maxFileSize {
            text = text[:maxFileSize] + "\n// ... truncated"
        }
        files = append(files, FileContent{Path: p, Content: text})
    }

    n.logger.Info("codenav complete", "files_fetched", len(files))
    return CodeContext{Files: files}, nil
}

func parseFileList(raw string) []string {
    // Reuse the balanced-bracket extraction from the phases package
    extracted := phases.ExtractJSONArray(raw)
    var paths []string
    if err := json.Unmarshal([]byte(extracted), &paths); err != nil {
        return nil
    }
    return paths
}

// truncateUTF8 shortens s to at most maxLen bytes, backing up to a valid
// UTF-8 rune boundary so multi-byte sequences are never split.
func truncateUTF8(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
        maxLen--
    }
    return s[:maxLen]
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/codenav/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/codenav/
git commit -m "feat: add LLM-based code navigation with on-demand file fetching"
```

---

## Task 4: Add code_navigation Capability to Butler Config

**Files:**
- Modify: `internal/config/butler.go`
- Modify: `internal/config/butler_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCodeNavigationDefault(t *testing.T) {
    c := DefaultConfig()
    if c.Capabilities.CodeNavigation {
        t.Error("code navigation should be disabled by default")
    }
    cfg, _ := Parse([]byte(`{"capabilities":{"code_navigation":true}}`))
    if !cfg.Capabilities.CodeNavigation {
        t.Error("code navigation should be enabled when set")
    }
}
```

- [ ] **Step 2: Run test — FAIL**

- [ ] **Step 3: Add field**

```go
// In Capabilities struct:
CodeNavigation bool `json:"code_navigation"`
```

Default remains `false` — opt-in per repo.

- [ ] **Step 4: Run test — PASS**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add code_navigation capability flag to butler.json"
```

---

## Task 5: Inject Code Context into Phase 2

**Files:**
- Modify: `internal/phases/phase2.go`
- Modify: `internal/phases/phase2_test.go` (if exists, otherwise test via integration)

Add an optional `codeContext` parameter to Phase 2 that gets appended to the LLM prompt when available. The system prompt is updated to tell the LLM it can reference source code.

- [ ] **Step 1: Modify Phase2 signature**

Add `codeContext string` as the last parameter to `Phase2()`. When non-empty, append it to the `userContent` string and update the system prompt to mention code is available.

```go
func Phase2(ctx context.Context, s store.PhaseQuerier, l llm.Provider, logger *slog.Logger, repo, title, body string, codeContext string) ([]Suggestion, error) {
```

- [ ] **Step 2: Update system prompt when code context is present**

When `codeContext != ""`, append to the system prompt:

```
You also have access to relevant source code from the repository. Use it to:
- Identify specific configuration options or code paths related to the bug
- Suggest specific debug log lines or config values the user should check
- Provide more targeted diagnostic steps based on the actual implementation
```

And append codeContext to userContent.

- [ ] **Step 3: Update all callers**

In `internal/webhook/handler.go`, pass `""` for now (wired up in Task 6):
```go
p2, err := phases.Phase2(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body, "")
```

Also update these callers to pass `""`:
- `internal/webhook/handler.go` retriage handler's Phase2 call
- `cmd/backfill/main.go` Phase2 call
- All test cases in `internal/phases/phase2_test.go` (add `""` as the last argument)

- [ ] **Step 4: Run all tests**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: accept optional code context in Phase 2 prompt"
```

---

## Task 6: Wire Up Code Navigation in Webhook Handler

**Files:**
- Modify: `internal/webhook/handler.go`

Connect the code navigator to the triage pipeline. When `cfg.Capabilities.CodeNavigation` is enabled, run the navigator before Phase 2 and pass the code context to the phase prompt.

- [ ] **Step 1: Add navigator field to Handler**

```go
type Handler struct {
    // ... existing fields
    codeNav *codenav.Navigator
}
```

Initialise in `New()`:
```go
codeNav := codenav.New(g, l, logger)
```

- [ ] **Step 2: Call navigator before Phase 2 when enabled**

In `handleOpened`, after determining `isBug` and before running Phase 2:

```go
var codeCtx string
if cfg.Capabilities.CodeNavigation {
    cc, err := h.codeNav.Navigate(ctx, installationID, dataRepo, issue.Title, issue.Body)
    if err != nil {
        issueLog.Error("code navigation failed", "error", err)
    } else {
        codeCtx = cc.FormatForPrompt()
    }
}
```

Then pass `codeCtx` to Phase 2:
```go
p2, err := phases.Phase2(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body, codeCtx)
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: wire up code navigation in triage pipeline"
```

---

## Task 7: Enable Code Navigation for teams-for-linux

**Files:**
- Modify: `.github/butler.json` in the `IsmaelMartinez/teams-for-linux` repo

- [ ] **Step 1: Update butler.json**

Add `"code_navigation": true` to the capabilities section of the butler.json in teams-for-linux. This is a config change in the target repo, not this repo.

- [ ] **Step 2: Test end-to-end**

Create a test issue or wait for the next real issue. Verify in Cloud Run logs that:
- `codenav start` appears
- `codenav tree fetched` shows the file count
- `codenav files identified` shows relevant files
- Phase 2 output references code-specific details

- [ ] **Step 3: Commit (in teams-for-linux repo)**

```bash
git commit -m "feat: enable code navigation for triage bot"
```

---

## Summary

| Task | Files | What it does |
|------|-------|--------------|
| 1 | `internal/github/client.go` | GetTree method for Git Trees API |
| 2 | `internal/codenav/tree.go` | Source file filtering and tree formatting |
| 3 | `internal/codenav/navigate.go` | LLM file identification + content fetching |
| 4 | `internal/config/butler.go` | code_navigation capability flag |
| 5 | `internal/phases/phase2.go`, `phase2_test.go`, `cmd/backfill/main.go` | Accept optional code context in prompt, update all callers |
| 6 | `internal/webhook/handler.go` | Wire navigator into triage pipeline |
| 7 | butler.json (teams-for-linux) | Enable for target repo |

Each task is independently testable and committable. The feature is opt-in via butler.json, so deploying the code has zero impact until the capability is enabled in the target repo. Phase 4a and agent research are out of scope for this iteration — extend after code navigation quality is validated on Phase 2.
