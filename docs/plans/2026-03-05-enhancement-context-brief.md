> **IMPLEMENTED**: Context brief flow is live. Enhancement issues get a lightweight brief with opt-in `research` signal for full synthesis.

# Enhancement Context Brief Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the Enhancement Researcher agent's default behavior from full research synthesis to a lightweight context brief that surfaces relevant ADRs, roadmap items, past issues, and documents via vector search, with an opt-in `research` signal to trigger the existing full pipeline.

**Architecture:** When an enhancement issue is opened, the agent creates a shadow issue as before, but posts a structured context brief (short LLM summary + raw vector search results organized by type) instead of entering the clarifying/researching flow. Two new signals (`research` and `use as context`) let the maintainer either trigger the existing full research pipeline or acknowledge they're taking the brief to Claude. The existing research, revision, PR, and publish flows are preserved and accessible via the `research` signal.

**Tech Stack:** Go 1.26, pgx/v5, Gemini API (existing `llm.Provider`), existing `store`, `github`, and `safety` packages.

---

### Task 1: Add new stage constant and signals

**Files:**
- Modify: `internal/store/models.go:63-71` — add `StageContextBrief`
- Modify: `internal/agent/orchestrator.go:1-35` — add `SignalResearch`, `SignalUseAsContext`
- Modify: `internal/agent/orchestrator_test.go` — add test cases

**Step 1: Write the failing test cases**

Add to `internal/agent/orchestrator_test.go` inside the `tests` slice:

```go
{"research", "research", SignalResearch},
{"research in sentence", "please start the research", SignalResearch},
{"use as context", "use as context", SignalUseAsContext},
{"using as context", "I'll use as context", SignalUseAsContext},
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestParseApprovalSignal -v`
Expected: FAIL — `SignalResearch` and `SignalUseAsContext` undefined.

**Step 3: Add stage constant to `internal/store/models.go`**

Add after `StageComplete = "complete"` (line 71):

```go
StageContextBrief = "context_brief"
```

**Step 4: Add signals and parsing to `internal/agent/orchestrator.go`**

Update the const block to add two new signals after `SignalPromote`:

```go
SignalResearch
SignalUseAsContext
```

Update `ParseApprovalSignal` to check for the new signals. Insert before the `SignalRevise` check (the `research` keyword must be checked before general keywords):

```go
if strings.Contains(normalized, "research") {
    return SignalResearch
}
if strings.Contains(normalized, "use as context") {
    return SignalUseAsContext
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestParseApprovalSignal -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/store/models.go internal/agent/orchestrator.go internal/agent/orchestrator_test.go
git commit -m "feat: add context_brief stage and research/use-as-context signals"
```

---

### Task 2: Build the context brief assembler

**Files:**
- Modify: `internal/agent/research.go` — add `BuildContextBrief` function and `ContextBrief` struct
- Create: `internal/agent/context_brief_test.go`

**Step 1: Write the failing test**

Create `internal/agent/context_brief_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestBuildContextBrief(t *testing.T) {
	mock := &mockProvider{
		response: `{"summary": "This enhancement requests dark mode support for the application UI."}`,
	}

	docs := []store.SimilarDocument{
		{Document: store.Document{DocType: "adr", Title: "ADR-005: Theme System", Content: "Decided to use CSS variables for theming. Status: accepted."}, Distance: 0.1},
		{Document: store.Document{DocType: "roadmap", Title: "UI Modernization", Content: "Planned refresh of the UI layer including theme support. Status: planned."}, Distance: 0.2},
	}
	issues := []store.SimilarIssue{
		{Issue: store.Issue{Number: 42, Title: "Support system dark mode", State: "closed", Summary: "User requested dark mode preference detection"}, Distance: 0.15},
		{Issue: store.Issue{Number: 99, Title: "High contrast mode", State: "open", Summary: "Accessibility improvement for visual themes"}, Distance: 0.25},
	}

	brief, err := BuildContextBrief(context.Background(), mock, "Add dark mode", "I want dark mode support", docs, issues, "owner/repo", 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if brief.Summary == "" {
		t.Error("expected non-empty summary")
	}

	md := FormatContextBriefMarkdown(brief)

	checks := []struct {
		label    string
		contains string
	}{
		{"header", "## Context Brief"},
		{"issue ref", "owner/repo#123"},
		{"summary present", brief.Summary},
		{"adr section", "### Architecture Decisions"},
		{"adr title", "ADR-005: Theme System"},
		{"roadmap section", "### Roadmap"},
		{"roadmap title", "UI Modernization"},
		{"issues section", "### Related Issues"},
		{"issue ref 42", "#42"},
		{"issue ref 99", "#99"},
		{"closed state", "closed"},
		{"open state", "open"},
		{"footer signals", "`research`"},
		{"footer use", "`use as context`"},
		{"footer reject", "`reject`"},
	}

	for _, c := range checks {
		if !strings.Contains(md, c.contains) {
			t.Errorf("%s: expected markdown to contain %q\n\nGot:\n%s", c.label, c.contains, md)
		}
	}
}

func TestBuildContextBrief_EmptyResults(t *testing.T) {
	mock := &mockProvider{
		response: `{"summary": "Enhancement request for a new feature."}`,
	}

	brief, err := BuildContextBrief(context.Background(), mock, "New feature", "Add a new thing", nil, nil, "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	md := FormatContextBriefMarkdown(brief)
	if strings.Contains(md, "### Architecture Decisions") {
		t.Error("should not show ADR section when no ADRs found")
	}
	if strings.Contains(md, "### Related Issues") {
		t.Error("should not show issues section when no issues found")
	}
	if !strings.Contains(md, brief.Summary) {
		t.Error("should still contain summary")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestBuildContextBrief -v`
Expected: FAIL — `BuildContextBrief` and `FormatContextBriefMarkdown` undefined.

**Step 3: Implement the context brief in `internal/agent/research.go`**

Add the following after the existing types and before `AnalyzeEnhancement`:

```go
// ContextBrief holds the assembled context for an enhancement request.
type ContextBrief struct {
	Summary    string
	SourceRepo string
	IssueNum   int
	Title      string
	ADRs       []store.SimilarDocument
	Roadmap    []store.SimilarDocument
	Research   []store.SimilarDocument
	Issues     []store.SimilarIssue
}

const contextBriefSummaryPrompt = `You are a technical analyst. Given an enhancement request title and body, write a 2-3 sentence summary of what is being requested and why it matters. Be concise and factual. Do not suggest solutions.

Respond with JSON: {"summary": "string"}`

// BuildContextBrief assembles a context brief from vector search results
// with a short LLM-generated summary of the enhancement request.
func BuildContextBrief(ctx context.Context, provider llm.Provider, title, body string, docs []store.SimilarDocument, issues []store.SimilarIssue, sourceRepo string, issueNumber int) (*ContextBrief, error) {
	userContent := fmt.Sprintf("Enhancement title: %s\n\nEnhancement body:\n%s", title, body)
	raw, err := provider.GenerateJSONWithSystem(ctx, contextBriefSummaryPrompt, userContent, 0.3, 512)
	if err != nil {
		return nil, fmt.Errorf("generate context brief summary: %w", err)
	}

	var result struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("parse context brief summary: %w", err)
	}

	brief := &ContextBrief{
		Summary:    result.Summary,
		SourceRepo: sourceRepo,
		IssueNum:   issueNumber,
		Title:      title,
		Issues:     issues,
	}

	// Partition documents by type
	for _, d := range docs {
		switch d.DocType {
		case "adr":
			brief.ADRs = append(brief.ADRs, d)
		case "roadmap":
			brief.Roadmap = append(brief.Roadmap, d)
		case "research":
			brief.Research = append(brief.Research, d)
		}
	}

	return brief, nil
}

// FormatContextBriefMarkdown renders a ContextBrief as a GitHub markdown comment.
func FormatContextBriefMarkdown(b *ContextBrief) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Context Brief: %s\n\n", b.Title)
	fmt.Fprintf(&sb, "> Context for %s#%d\n\n", b.SourceRepo, b.IssueNum)
	sb.WriteString(b.Summary)
	sb.WriteString("\n")

	if len(b.ADRs) > 0 {
		sb.WriteString("\n### Architecture Decisions\n\n")
		for _, d := range b.ADRs {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", d.Title, truncate(d.Content, 500))
		}
	}

	if len(b.Roadmap) > 0 {
		sb.WriteString("\n### Roadmap\n\n")
		for _, d := range b.Roadmap {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", d.Title, truncate(d.Content, 500))
		}
	}

	if len(b.Research) > 0 {
		sb.WriteString("\n### Prior Research\n\n")
		for _, d := range b.Research {
			fmt.Fprintf(&sb, "**%s**\n\n%s\n\n", d.Title, truncate(d.Content, 500))
		}
	}

	if len(b.Issues) > 0 {
		sb.WriteString("\n### Related Issues\n\n")
		for _, i := range b.Issues {
			state := i.State
			summary := truncate(i.Summary, 200)
			fmt.Fprintf(&sb, "- #%d **%s** (%s) — %s\n", i.Number, i.Title, state, summary)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n---\n\n")
	sb.WriteString("Reply `research` to trigger full Gemini research synthesis, `use as context` to acknowledge, or `reject` to close.\n")

	return sb.String()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestBuildContextBrief -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/research.go internal/agent/context_brief_test.go
git commit -m "feat: add context brief assembler for enhancement issues"
```

---

### Task 3: Update `StartSession` to post context brief

**Files:**
- Modify: `internal/agent/handler.go:44-81` — replace the analyze-then-clarify/research flow with context brief

**Step 1: Rewrite `StartSession` in `internal/agent/handler.go`**

Replace the current `StartSession` method body (lines 44-81) with:

```go
func (h *AgentHandler) StartSession(ctx context.Context, installationID int64, sourceRepo string, issueNumber int, shadowRepo string, title, body string) error {
	log := h.logger.With("sourceRepo", sourceRepo, "issue", issueNumber, "shadowRepo", shadowRepo)

	// Create mirror issue in shadow repo
	shadowBody := gh.FormatShadowIssueBody(sourceRepo, issueNumber, title, body)
	shadowNumber, err := h.github.CreateIssue(ctx, installationID, shadowRepo, "[Research] "+title, shadowBody)
	if err != nil {
		return fmt.Errorf("create shadow issue: %w", err)
	}
	log = log.With("shadowIssue", shadowNumber)
	log.Info("created shadow issue")

	// Create session
	sessionID, err := h.store.CreateSession(ctx, store.AgentSession{
		Repo:              sourceRepo,
		IssueNumber:       issueNumber,
		ShadowRepo:        shadowRepo,
		ShadowIssueNumber: shadowNumber,
		Stage:             store.StageNew,
		Context:           map[string]any{"title": title, "body": body},
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	log = log.With("sessionID", sessionID)

	// Embed title+body for vector search
	embedding, err := h.llm.Embed(ctx, fmt.Sprintf("%s\n%s", title, body))
	if err != nil {
		return fmt.Errorf("embed issue: %w", err)
	}

	// Search for similar docs and issues
	similarDocs, err := h.store.FindSimilarDocuments(ctx, sourceRepo, store.EnhancementDocTypes, embedding, 5)
	if err != nil {
		log.Error("find similar documents", "error", err)
	}
	similarIssues, err := h.store.FindSimilarIssues(ctx, sourceRepo, embedding, issueNumber, 5)
	if err != nil {
		log.Error("find similar issues", "error", err)
	}

	// Build context brief
	brief, err := BuildContextBrief(ctx, h.llm, title, body, similarDocs, similarIssues, sourceRepo, issueNumber)
	if err != nil {
		return fmt.Errorf("build context brief: %w", err)
	}

	briefMD := FormatContextBriefMarkdown(brief)

	// Run structural safety check
	structResult := h.structural.Validate(briefMD)
	if !structResult.Passed {
		log.Error("structural safety check failed for context brief", "reason", structResult.Reason)
		return fmt.Errorf("structural safety check failed: %s", structResult.Reason)
	}

	// Run LLM safety check
	issueContext := fmt.Sprintf("Enhancement: %s\n\n%s", title, body)
	llmResult := h.llmSafety.ValidateWithContext(ctx, briefMD, issueContext)
	if !llmResult.Passed {
		log.Error("LLM safety check failed for context brief", "reason", llmResult.Reason)
		return fmt.Errorf("LLM safety check failed: %s", llmResult.Reason)
	}

	// Post on shadow issue
	_, err = h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, briefMD)
	if err != nil {
		return fmt.Errorf("post context brief: %w", err)
	}

	// Update session to context_brief stage
	if err := h.store.UpdateSessionStage(ctx, sessionID, store.StageContextBrief, map[string]any{
		"title": title, "body": body,
	}, 0); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Create audit entry
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sessionID,
		ActionType:        "posted_context_brief",
		InputHash:         hashString(title + body),
		OutputSummary:     truncate(briefMD, 200),
		SafetyCheckPassed: true,
		ConfidenceScore:   llmResult.Confidence,
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	log.Info("posted context brief", "docs", len(similarDocs), "issues", len(similarIssues))
	return nil
}
```

**Step 2: Build**

Run: `go build ./internal/agent/...`
Expected: success. The `askClarifyingQuestions` and `startResearch` methods remain — they're still called by `HandleComment` for the `research` signal path.

**Step 3: Run tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/handler.go
git commit -m "feat: post context brief instead of full research on session start"
```

---

### Task 4: Handle `research` and `use as context` signals in `HandleComment`

**Files:**
- Modify: `internal/agent/handler.go:273-309` — add `StageContextBrief` case to the switch

**Step 1: Update the `HandleComment` switch in `handler.go`**

In the `switch sess.Stage` block (around line 294), add a new case before `default`:

```go
case store.StageContextBrief:
    return h.handleContextBriefResponse(ctx, installationID, sess, signal, commentBody, commentUser, log)
```

**Step 2: Add `handleContextBriefResponse` method**

Add after the `handleReject` method:

```go
func (h *AgentHandler) handleContextBriefResponse(ctx context.Context, installationID int64, sess *store.AgentSession, signal ApprovalSignal, commentBody string, commentUser string, log *slog.Logger) error {
	switch signal {
	case SignalResearch:
		log.Info("research requested, starting full research pipeline")
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, nil)

	case SignalUseAsContext:
		log.Info("context brief acknowledged, closing session")
		if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, sess.RoundTripCount); err != nil {
			return fmt.Errorf("update session stage: %w", err)
		}
		ack := "Context brief acknowledged. Session closed."
		_, _ = h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, ack)
		if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
			SessionID:         sess.ID,
			ActionType:        "context_acknowledged",
			InputHash:         hashString(commentBody),
			OutputSummary:     "Session closed after context brief acknowledged",
			SafetyCheckPassed: true,
			ConfidenceScore:   1.0,
		}); err != nil {
			log.Error("create audit entry", "error", err)
		}
		return nil

	default:
		log.Info("ignoring non-signal comment on context brief")
		return nil
	}
}
```

**Step 3: Build and test**

Run: `go build ./internal/agent/... && go test ./internal/agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/handler.go
git commit -m "feat: handle research and use-as-context signals on context brief"
```

---

### Task 5: Add integration-style test for the context brief flow

**Files:**
- Modify: `internal/agent/integration_test.go` — add context brief test if the file has existing patterns, or create a new test

**Step 1: Read `integration_test.go` to understand the existing test pattern**

Run: `cat internal/agent/integration_test.go`

Check what mocks and patterns are used. The test should verify:
1. `StartSession` posts a context brief (not research) to the shadow issue
2. `HandleComment` with `research` signal transitions to the research flow
3. `HandleComment` with `use as context` signal completes the session
4. `HandleComment` with `reject` signal completes the session

**Step 2: Write the test**

The test approach depends on what's in `integration_test.go`. If the file uses real store/GitHub mocks, follow that pattern. If it's a compile-time check or stub, write a focused unit test instead.

At minimum, add a test that verifies `ParseApprovalSignal("research") == SignalResearch` and `ParseApprovalSignal("use as context") == SignalUseAsContext` — these are already covered by Task 1. The more valuable test verifies `handleContextBriefResponse` routes correctly, but this requires mocking the store and GitHub client.

If the existing integration test has a mock store, add:

```go
func TestContextBriefFlow_ResearchSignal(t *testing.T) {
    // Verify that a session in StageContextBrief transitions to research
    // when receiving SignalResearch
}

func TestContextBriefFlow_UseAsContextSignal(t *testing.T) {
    // Verify that a session in StageContextBrief completes
    // when receiving SignalUseAsContext
}
```

Follow the patterns from the existing test file.

**Step 3: Run all tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/integration_test.go
git commit -m "test: add context brief flow tests"
```

---

### Task 6: Run linter and full test suite

**Files:**
- No new files — validation only

**Step 1: Run vet**

Run: `go vet ./...`
Expected: no issues

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: PASS (or fix any issues found)

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: all pass

**Step 4: Commit any linter fixes**

```bash
git add -A
git commit -m "chore: fix lint issues"
```

(Skip this commit if no changes were needed.)

---

### Task 7: Update documentation

**Files:**
- Modify: `CLAUDE.md` — update agent description to mention context brief
- Modify: `README.md` — update Enhancement Researcher Agent section

**Step 1: Update `CLAUDE.md`**

In the Architecture section, update the agent description to reflect that the default flow posts a context brief (structured context dump from vector search) instead of launching into full research. Mention the `research` signal for opting into full synthesis.

**Step 2: Update `README.md`**

Update the Enhancement Researcher Agent section to describe the two-mode flow: context brief by default, full research on demand via `research` signal. Keep the existing description of the full research pipeline but note it's triggered by the `research` signal.

**Step 3: Build to verify no broken references**

Run: `go build ./...`

**Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: update agent docs for context brief default"
```

---

## Execution Options

Plan saved to `docs/plans/2026-03-05-enhancement-context-brief.md`.

**1. Subagent-Driven (this session)** — fresh subagent per task, code review between tasks, fast iteration.

**2. Parallel Session (separate)** — open a new session in this worktree and use `superpowers:executing-plans`.

Which approach?
