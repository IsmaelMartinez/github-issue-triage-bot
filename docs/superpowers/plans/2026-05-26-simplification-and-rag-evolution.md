# Simplification and RAG Evolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove shadow posting, switch to silent RAG mode, seed Electron v42, strip the dashboard, and evolve the retrieval engine with version-aware boosting, recency weighting, and LLM keyword extraction.

**Architecture:** Two-phase delivery. Phase 1 simplifies the bot to an embed-only webhook handler with a stripped dashboard. Phase 2 adds version lifecycle management and three retrieval improvements to `brief-preview`. Both phases produce independently deployable PRs.

**Tech Stack:** Go, PostgreSQL/pgvector, Gemini Flash (embedding + keyword extraction), GitHub Actions workflows, Terraform env vars.

---

## Phase 1: Simplification

### Task 1: Remove shadow posting and switch to silent RAG mode

**Files:**
- Modify: `.github/workflows/deploy.yml:76`
- Modify: `internal/webhook/handler.go:416-600`

- [ ] **Step 1: Write test for embed-only webhook path**

Add a test case in `internal/webhook/handler_test.go` that verifies when an issue webhook arrives, `UpsertIssue` is called but no comment is created and no triage session is recorded. This validates the silent RAG mode.

```go
func TestHandleIssueEvent_SilentRAGMode(t *testing.T) {
	h := newTestHandler(t)
	// Ensure no shadow repos configured
	h.shadowRepos = map[string]string{}

	issue := testIssue("App crashes on startup", "### Describe the bug\n\nCrash\n\n### Reproduction steps\n\n1. Open\n\n### Expected Behavior\n\nNo crash\n\n### Debug\n\nlogs here")
	h.handleIssueOpened("IsmaelMartinez/teams-for-linux", issue, 12345)

	// Issue should be embedded
	if !h.store.UpsertIssueCalled {
		t.Error("expected UpsertIssue to be called")
	}
	// No comment should be posted
	if h.github.CreateCommentCalled {
		t.Error("expected no comment in silent RAG mode")
	}
	// No triage session should be created
	if h.store.CreateTriageSessionCalled {
		t.Error("expected no triage session in silent RAG mode")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/webhook/ -run TestHandleIssueEvent_SilentRAGMode -v`
Expected: FAIL (handler still runs full pipeline)

- [ ] **Step 3: Remove TF_VAR_shadow_repos from deploy.yml**

In `.github/workflows/deploy.yml`, remove line 76:

```yaml
# REMOVE this line:
          TF_VAR_shadow_repos: "IsmaelMartinez/teams-for-linux:IsmaelMartinez/teams-for-linux-shadow,IsmaelMartinez/triage-bot-test-repo:IsmaelMartinez/triage-bot-test-repo-shadow"
```

- [ ] **Step 4: Simplify handleIssueEvent to embed-only**

In `internal/webhook/handler.go`, after the `upsertIssue` call at line 417, return early. Replace lines 419-595 (everything from `// Use sourceRepo for data lookups` through the end of the shadow/direct posting block) with:

```go
	// Silent RAG mode: embed and return. The full triage pipeline only
	// runs on demand via /brief-preview.
	issueLog.Info("issue embedded (silent RAG mode)")
	return
```

Also remove the enhancement agent session block starting at line 598 (`if isEnhancement && cfg.Capabilities.Research`), since it depends on shadow repos.

- [ ] **Step 5: Remove the /retriage comment command handler**

Find the `handleCommentEvent` function that processes `/retriage` commands and remove the retriage logic. The `/retriage` command has no purpose in silent mode.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/webhook/ -run TestHandleIssueEvent_SilentRAGMode -v`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: All tests pass (some existing tests for shadow posting may need updating or removal)

- [ ] **Step 8: Commit**

```bash
git add .github/workflows/deploy.yml internal/webhook/handler.go internal/webhook/handler_test.go
git commit -m "feat: remove shadow posting, switch to silent RAG mode"
```

---

### Task 2: Strip dashboard of shadow-specific metrics

**Files:**
- Modify: `internal/store/report.go:16-36` (DashboardStats struct)
- Modify: `internal/store/report.go:207-391` (GetDashboardStats function)
- Modify: `cmd/server/template.html`
- Modify: `.github/workflows/dashboard.yml`

- [ ] **Step 1: Write test for simplified DashboardStats**

```go
func TestGetDashboardStats_NoShadowFields(t *testing.T) {
	// Verify that DashboardStats no longer includes TriageStats, AgentStats, ApprovalGateStats
	stats := &store.DashboardStats{}
	data, _ := json.Marshal(stats)
	var m map[string]any
	json.Unmarshal(data, &m)
	for _, field := range []string{"triage_stats", "agent_stats", "approval_gate_stats"} {
		if _, ok := m[field]; ok {
			t.Errorf("DashboardStats should not contain %s after simplification", field)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestGetDashboardStats_NoShadowFields -v`
Expected: FAIL (fields still present)

- [ ] **Step 3: Remove shadow-specific fields from DashboardStats**

In `internal/store/report.go`, remove these fields from `DashboardStats`:
- `TriageStats *TriageStats` (line 24)
- `AgentStats *AgentStats` (line 25)
- `ApprovalGateStats []GateOutcome` (line 27)

Remove the associated struct types: `TriageStats`, `RecentTriage`, `AgentStats`, `RecentAgent`, `GateOutcome`.

Remove the helper methods: `getTriageStats`, `getAgentStats`, `getApprovalGateStats`.

In `GetDashboardStats`, remove the calls to those three methods (lines 296-315).

Also remove `AvgResponseSeconds` (line 26) and its query (lines 333-342), since it computed time-to-first-response from triage sessions which no longer exist.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestGetDashboardStats_NoShadowFields -v`
Expected: PASS

- [ ] **Step 5: Update dashboard.yml — remove test repo and cleanup step**

In `.github/workflows/dashboard.yml`:
- Change the default repos on lines 9, 38, 52 from `'IsmaelMartinez/teams-for-linux,IsmaelMartinez/triage-bot-test-repo'` to `'IsmaelMartinez/teams-for-linux'`
- Remove the "Close stale shadow issues" step (the `curl` to `/cleanup`)

- [ ] **Step 6: Update template.html — remove shadow panels**

Remove the shadow-specific dashboard panels from `cmd/server/template.html`: triage outcomes chart, agent sessions chart, approval gate chart, and any shadow issue links. Keep: document counts, issue count, phase hit rates, feedback stats, daily volume charts, response time distribution.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/store/report.go cmd/server/template.html .github/workflows/dashboard.yml
git commit -m "feat: strip dashboard of shadow-specific metrics"
```

---

### Task 3: Seed Electron v42 data

**Files:**
- Create: `data/electron-v42-issues.json`
- Create: `data/electron-v42-releases.json`
- Modify: `.github/workflows/upstream-refresh.yml:41`

- [ ] **Step 1: Generate Electron v42 release seed data**

```bash
./scripts/generate-upstream-index.sh \
  --repo electron/electron \
  --type releases \
  --version 42 \
  --doc-type upstream_release \
  > data/electron-v42-releases.json
```

- [ ] **Step 2: Generate Electron v42 issue seed data**

```bash
./scripts/generate-upstream-index.sh \
  --repo electron/electron \
  --type issues \
  --version "42-x-y" \
  --doc-type upstream_issue \
  > data/electron-v42-issues.json
```

- [ ] **Step 3: Verify seed files are valid JSON**

```bash
python3 -c "import json; d=json.load(open('data/electron-v42-releases.json')); print(f'{len(d)} releases')"
python3 -c "import json; d=json.load(open('data/electron-v42-issues.json')); print(f'{len(d)} issues')"
```

Expected: Non-zero counts for both.

- [ ] **Step 4: Update upstream-refresh.yml default versions**

In `.github/workflows/upstream-refresh.yml` line 41, change:
```yaml
        default: '40,41'
```
to:
```yaml
        default: '40,41,42'
```

- [ ] **Step 5: Commit**

```bash
git add data/electron-v42-issues.json data/electron-v42-releases.json .github/workflows/upstream-refresh.yml
git commit -m "data: seed Electron v42 upstream docs"
```

---

### Task 4: Port edge-case test patterns from test repo

**Files:**
- Modify: `internal/phases/phase1_test.go`

- [ ] **Step 1: Add Spanish-language issue test fixture**

In `internal/phases/phase1_test.go`, add a test case to `TestPhase1` for a non-English issue body:

```go
{
	name: "non-English issue body (Spanish)",
	body: "### Describe the bug\n\nLa aplicación no inicia en Ubuntu 24.04. Muestra una pantalla en blanco.\n\n### Reproduction steps\n\n1. Instalar el paquete\n2. Ejecutar la aplicación\n\n### Expected Behavior\n\nLa aplicación debería iniciar correctamente\n\n### Debug\n\n_No response_",
	want: phases.Phase1Result{
		MissingItems: []string{"debug"},
	},
},
```

- [ ] **Step 2: Add injection payload test fixture**

```go
{
	name: "injection payloads in body",
	body: "### Describe the bug\n\n<script>alert(1)</script> App crashes\n\n### Reproduction steps\n\n1. Open [link](javascript:alert(1))\n2. Click <img src=x onerror=alert(1)>\n\n### Expected Behavior\n\nNo crash\n\n### Debug\n\ndata:text/html,<script>alert(1)</script>",
	want: phases.Phase1Result{
		MissingItems: []string{},
	},
},
```

- [ ] **Step 3: Add enhancement-through-bug-template test fixture**

```go
{
	name: "enhancement filed via bug template scores low",
	body: "### Describe the bug\n\n_No response_\n\n### Reproduction steps\n\n_No response_\n\n### Expected Behavior\n\nBetter search functionality\n\n### Debug\n\n_No response_",
	want: phases.Phase1Result{
		MissingItems: []string{"description", "steps", "debug"},
	},
},
```

- [ ] **Step 4: Run phase1 tests**

Run: `go test ./internal/phases/ -run TestPhase1 -v`
Expected: PASS (adjust expected `MissingItems` to match actual Phase1 logic if needed)

- [ ] **Step 5: Commit**

```bash
git add internal/phases/phase1_test.go
git commit -m "test: port edge-case fixtures from test repo before archiving"
```

---

## Phase 2: RAG Evolution

### Task 5: Add electron_version to seed metadata

**Files:**
- Modify: `cmd/seed/main.go:190-247` (seedFeatures function)
- Modify: `internal/store/models.go`

- [ ] **Step 1: Write test for version extraction from seed entry**

Create `cmd/seed/version_test.go`:

```go
package main

import "testing"

func TestExtractElectronVersion(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{"v41.0.0 Release Notes", "41"},
		{"v39.8.2 Patch Release", "39"},
		{"Electron 42.1.0 breaking change: clipboard API", "42"},
		{"No version here", ""},
		{"Fix crash in BrowserWindow v40.3.1", "40"},
	}
	for _, tt := range tests {
		got := extractElectronVersion(tt.topic)
		if got != tt.want {
			t.Errorf("extractElectronVersion(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/seed/ -run TestExtractElectronVersion -v`
Expected: FAIL (function not defined)

- [ ] **Step 3: Implement extractElectronVersion**

Create `cmd/seed/version.go`:

```go
package main

import "regexp"

var electronVersionRe = regexp.MustCompile(`v?(\d+)\.\d+`)

func extractElectronVersion(topic string) string {
	m := electronVersionRe.FindStringSubmatch(topic)
	if m == nil {
		return ""
	}
	return m[1]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/seed/ -run TestExtractElectronVersion -v`
Expected: PASS

- [ ] **Step 5: Wire version into seedFeatures metadata**

In `cmd/seed/main.go`, in the `seedFeatures` function, after building the `doc` struct (line 222-237), add:

```go
		if ver := extractElectronVersion(e.Topic); ver != "" {
			doc.Metadata["electron_version"] = ver
		}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/seed/version.go cmd/seed/version_test.go cmd/seed/main.go
git commit -m "feat: add electron_version to seed metadata"
```

---

### Task 6: Implement DeleteDocumentsByVersion store method

**Files:**
- Modify: `internal/store/postgres.go`
- Create: `internal/store/postgres_version_test.go`

- [ ] **Step 1: Write test for DeleteDocumentsByVersion**

Create `internal/store/postgres_version_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestDeleteDocumentsByVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed two documents with different versions
	s.UpsertDocument(ctx, store.Document{
		Repo: "test/repo", DocType: "upstream_release", Title: "v39.0.0",
		Content: "old release", Metadata: map[string]any{"electron_version": "39"},
		Embedding: testEmbedding(),
	})
	s.UpsertDocument(ctx, store.Document{
		Repo: "test/repo", DocType: "upstream_issue", Title: "v41 crash",
		Content: "new issue", Metadata: map[string]any{"electron_version": "41"},
		Embedding: testEmbedding(),
	})

	// Delete version 39
	n, err := s.DeleteDocumentsByVersion(ctx, "test/repo", store.UpstreamDocTypes, "39")
	if err != nil {
		t.Fatalf("DeleteDocumentsByVersion: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}

	// v41 document should still exist
	docs, _ := s.FindSimilarDocuments(ctx, "test/repo", store.UpstreamDocTypes, testEmbedding(), 10)
	if len(docs) != 1 {
		t.Errorf("expected 1 remaining doc, got %d", len(docs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestDeleteDocumentsByVersion -v`
Expected: FAIL (method not defined)

- [ ] **Step 3: Implement DeleteDocumentsByVersion**

In `internal/store/postgres.go`:

```go
// DeleteDocumentsByVersion removes all documents matching the given doc types
// whose metadata contains the specified electron_version. Returns the count
// of deleted rows.
func (s *Store) DeleteDocumentsByVersion(ctx context.Context, repo string, docTypes []string, version string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM documents
		WHERE repo = $1
		AND doc_type = ANY($2)
		AND metadata->>'electron_version' = $3
	`, repo, docTypes, version)
	if err != nil {
		return 0, fmt.Errorf("delete by version: %w", err)
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestDeleteDocumentsByVersion -v`
Expected: PASS

- [ ] **Step 5: Add cleanup subcommand to seed CLI**

In `cmd/seed/main.go`, add a `"cleanup"` case to the switch:

```go
	case "cleanup":
		err = cleanupOldVersions(ctx, s, repo, logger)
```

Create `cmd/seed/cleanup.go`:

```go
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
```

- [ ] **Step 6: Wire cleanup into upstream-refresh.yml**

At the end of the refresh workflow, after the seed loop, add:

```yaml
      - name: Cleanup old Electron versions
        env:
          DATABASE_URL: ${{ secrets.TF_VAR_DATABASE_URL }}
          ACTIVE_VERSIONS: ${{ inputs.versions || '40,41,42' }}
        run: ./seed cleanup
```

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/store/postgres.go internal/store/postgres_version_test.go cmd/seed/main.go cmd/seed/cleanup.go cmd/seed/version.go cmd/seed/version_test.go .github/workflows/upstream-refresh.yml
git commit -m "feat: implement Electron version lifecycle with N-2 cleanup"
```

---

### Task 7: Version-aware retrieval boosting

**Files:**
- Modify: `cmd/server/brief_preview.go:100-108`
- Modify: `internal/store/rerank.go`

- [ ] **Step 1: Write test for version-aware document reranking**

Create `internal/store/rerank_test.go` (or add to existing):

```go
func TestApplyVersionBoost(t *testing.T) {
	docs := []store.SimilarDocument{
		{Document: store.Document{Title: "v39 release", Metadata: map[string]any{"electron_version": "39"}}, Distance: 0.3},
		{Document: store.Document{Title: "v41 release", Metadata: map[string]any{"electron_version": "41"}}, Distance: 0.35},
		{Document: store.Document{Title: "v40 release", Metadata: map[string]any{"electron_version": "40"}}, Distance: 0.32},
	}
	result := store.ApplyVersionBoost(docs, "41", 0.05, 0.02)
	// v41 (exact match) should be first despite higher original distance
	if result[0].Title != "v41 release" {
		t.Errorf("expected v41 first, got %s", result[0].Title)
	}
	// v40 (adjacent) should get smaller boost
	if result[1].Title != "v40 release" {
		t.Errorf("expected v40 second, got %s", result[1].Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestApplyVersionBoost -v`
Expected: FAIL

- [ ] **Step 3: Implement ApplyVersionBoost**

In `internal/store/rerank.go`:

```go
// ApplyVersionBoost rescales distances downward for documents whose metadata
// electron_version matches the target version. Exact matches get the full
// boost; adjacent versions (N-1, N+1) get adjacentBoost.
func ApplyVersionBoost(docs []SimilarDocument, targetVersion string, exactBoost, adjacentBoost float64) []SimilarDocument {
	if targetVersion == "" || (exactBoost <= 0 && adjacentBoost <= 0) {
		return docs
	}
	target, err := strconv.Atoi(targetVersion)
	if err != nil {
		return docs
	}

	type scored struct {
		doc      SimilarDocument
		adjusted float64
	}
	out := make([]scored, len(docs))
	for i, d := range docs {
		adj := d.Distance
		if v, ok := d.Metadata["electron_version"].(string); ok {
			if n, err := strconv.Atoi(v); err == nil {
				switch {
				case n == target:
					adj -= exactBoost
				case n == target-1 || n == target+1:
					adj -= adjacentBoost
				}
			}
		}
		if adj < 0 {
			adj = 0
		}
		out[i] = scored{doc: d, adjusted: adj}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].adjusted < out[j].adjusted })
	result := make([]SimilarDocument, len(out))
	for i, s := range out {
		result[i] = s.doc
		result[i].Distance = s.adjusted
	}
	return result
}
```

Add `"strconv"` to the imports in `rerank.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestApplyVersionBoost -v`
Expected: PASS

- [ ] **Step 5: Wire version boost into brief_preview.go**

In `cmd/server/brief_preview.go`, add an Electron version extraction regex and call `ApplyVersionBoost` after the hat boost:

```go
var electronVersionFromBodyRe = regexp.MustCompile(`(?i)[Ee]lectron[\s:]+v?(\d+)`)
```

In `briefPreviewHandler`, after the hat boost on docs (line 108), add:

```go
	if m := electronVersionFromBodyRe.FindStringSubmatch(issue.Body); m != nil {
		docs = store.ApplyVersionBoost(docs, m[1], 0.05, 0.02)
	}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/store/rerank.go internal/store/rerank_test.go cmd/server/brief_preview.go
git commit -m "feat: version-aware retrieval boosting for upstream docs"
```

---

### Task 8: Recency weighting on similar-issue queries

**Files:**
- Modify: `cmd/server/brief_preview.go:110-114`
- Modify: `internal/store/rerank.go`

- [ ] **Step 1: Write test for recency reranking**

In `internal/store/rerank_test.go`:

```go
func TestApplyRecencyBoost(t *testing.T) {
	now := time.Now()
	issues := []store.SimilarIssue{
		{Issue: store.Issue{Title: "old issue", CreatedAt: now.AddDate(-2, 0, 0)}, Distance: 0.2},
		{Issue: store.Issue{Title: "recent issue", CreatedAt: now.AddDate(0, -3, 0)}, Distance: 0.25},
		{Issue: store.Issue{Title: "mid issue", CreatedAt: now.AddDate(0, -9, 0)}, Distance: 0.22},
	}
	result := store.ApplyRecencyBoost(issues, 0.05, 0.02)
	// Recent (< 6 months) gets biggest boost despite higher distance
	if result[0].Title != "recent issue" {
		t.Errorf("expected recent issue first, got %s", result[0].Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestApplyRecencyBoost -v`
Expected: FAIL

- [ ] **Step 3: Implement ApplyRecencyBoost**

In `internal/store/rerank.go`:

```go
const (
	RecencyRecentMonths = 6
	RecencyMidMonths    = 12
)

// ApplyRecencyBoost rescales distances downward for issues based on age.
// Issues less than RecencyRecentMonths old get recentBoost; issues between
// RecencyRecentMonths and RecencyMidMonths get midBoost; older issues get
// no adjustment.
func ApplyRecencyBoost(issues []SimilarIssue, recentBoost, midBoost float64) []SimilarIssue {
	if recentBoost <= 0 && midBoost <= 0 {
		return issues
	}
	now := time.Now()
	recentCutoff := now.AddDate(0, -RecencyRecentMonths, 0)
	midCutoff := now.AddDate(0, -RecencyMidMonths, 0)

	type scored struct {
		issue    SimilarIssue
		adjusted float64
	}
	out := make([]scored, len(issues))
	for i, iss := range issues {
		adj := iss.Distance
		switch {
		case iss.CreatedAt.After(recentCutoff):
			adj -= recentBoost
		case iss.CreatedAt.After(midCutoff):
			adj -= midBoost
		}
		if adj < 0 {
			adj = 0
		}
		out[i] = scored{issue: iss, adjusted: adj}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].adjusted < out[j].adjusted })
	result := make([]SimilarIssue, len(out))
	for i, s := range out {
		result[i] = s.issue
		result[i].Distance = s.adjusted
	}
	return result
}
```

Add `"time"` to the imports in `rerank.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestApplyRecencyBoost -v`
Expected: PASS

- [ ] **Step 5: Wire recency boost into brief_preview.go**

In `cmd/server/brief_preview.go`, after the `FindSimilarIssues` call (line 110-114), add:

```go
	if similar != nil {
		similar = store.ApplyRecencyBoost(similar, 0.05, 0.02)
	}
```

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/store/rerank.go internal/store/rerank_test.go cmd/server/brief_preview.go
git commit -m "feat: recency weighting on similar-issue retrieval"
```

---

### Task 9: LLM-based symptom keyword extraction

**Files:**
- Modify: `cmd/server/brief_preview.go:195-205`
- Create: `cmd/server/brief_preview_keywords_test.go`

- [ ] **Step 1: Write test for LLM keyword extraction fallback**

Create `cmd/server/brief_preview_keywords_test.go`:

```go
package main

import "testing"

func TestExtractSymptomKeywords_Fallback(t *testing.T) {
	// When LLM extraction returns empty, fall back to hardcoded
	body := "wayland screen sharing is broken"
	got := extractSymptomKeywords(body)
	if len(got) == 0 {
		t.Error("expected at least 'wayland' and 'screen' from fallback")
	}
}
```

- [ ] **Step 2: Run test to verify it passes (existing behavior)**

Run: `go test ./cmd/server/ -run TestExtractSymptomKeywords_Fallback -v`
Expected: PASS (this validates the fallback still works)

- [ ] **Step 3: Add LLM extraction with fallback**

In `cmd/server/brief_preview.go`, replace `extractSymptomKeywords`:

```go
// extractSymptomKeywordsLLM asks Gemini Flash to extract technical keywords
// from the issue body for regression PR filtering. Falls back to the
// hardcoded list if the LLM call fails or returns empty.
func (srv *server) extractSymptomKeywordsLLM(ctx context.Context, body string) []string {
	prompt := `Extract 3-8 technical keywords from this GitHub issue body that would help find relevant pull requests. Focus on: component names, API names, error messages, platform terms, Electron subsystem names. Return JSON: {"keywords": ["word1", "word2"]}`

	result, err := srv.llm.GenerateJSON(ctx, prompt+"\n\nIssue body:\n"+body, 0.1, 256)
	if err == nil {
		var parsed struct {
			Keywords []string `json:"keywords"`
		}
		if json.Unmarshal([]byte(result), &parsed) == nil && len(parsed.Keywords) > 0 {
			return parsed.Keywords
		}
	}
	return extractSymptomKeywords(body)
}
```

- [ ] **Step 4: Wire LLM extraction into briefPreviewHandler**

In `briefPreviewHandler`, replace the call to `extractSymptomKeywords(issue.Body)` (line 125) with:

```go
				keywords := srv.extractSymptomKeywordsLLM(ctx, issue.Body)
```

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/server/brief_preview.go cmd/server/brief_preview_keywords_test.go
git commit -m "feat: LLM-based symptom keyword extraction with hardcoded fallback"
```

---

### Task 10: Re-seed existing versions with electron_version metadata

**Files:**
- No new files — uses existing seed workflow

- [ ] **Step 1: Re-run seed for v39, v40, v41 to add electron_version metadata**

After deploying the `electron_version` metadata change from Task 5, trigger the seed workflow for all existing versions to backfill the metadata:

```bash
gh workflow run upstream-refresh.yml -f versions="39,40,41,42" --repo IsmaelMartinez/github-issue-triage-bot
```

- [ ] **Step 2: Verify metadata was added**

Query the store to confirm `electron_version` is present:

```bash
curl -sf -H "Authorization: Bearer $TOKEN" \
  "https://triage-bot-lhuutxzbnq-uc.a.run.app/report?repo=IsmaelMartinez/teams-for-linux" \
  | jq '.document_counts'
```

- [ ] **Step 3: Run cleanup to drop v39**

```bash
gh workflow run upstream-refresh.yml -f versions="40,41,42" --repo IsmaelMartinez/github-issue-triage-bot
```

The cleanup step should detect v39 as > 2 majors behind v42 and remove it.

---

### Task 11: Archive repos (manual)

After Phase 1 PR is merged and verified:

- [ ] **Step 1: Archive triage-bot-test-repo**

GitHub Settings > Danger Zone > Archive this repository for `IsmaelMartinez/triage-bot-test-repo`

- [ ] **Step 2: Archive triage-bot-test-repo-shadow**

GitHub Settings > Danger Zone > Archive this repository for `IsmaelMartinez/triage-bot-test-repo-shadow`

- [ ] **Step 3: Archive teams-for-linux-shadow**

GitHub Settings > Danger Zone > Archive this repository for `IsmaelMartinez/teams-for-linux-shadow`
