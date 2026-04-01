# Security Hardening — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address all HIGH and CRITICAL findings from the four-agent security audit, plus high-value MEDIUM fixes that are low effort.

**Architecture:** Fixes are grouped into 6 tasks by theme: endpoint auth, panic recovery, sanitisation gaps, delivery ID enforcement, repo validation, and CI/workflow hardening. Each task is independently committable and testable. No new dependencies.

**Tech Stack:** Go 1.26.1, standard library only. Existing test patterns (table-driven, no mocks).

**Audit source:** Four parallel security audits (webhook auth, LLM injection, infrastructure, exec/DoS) conducted 2026-04-01.

---

## File Structure

```
cmd/server/main.go                    # Modified: add auth to /cleanup, /health-check; validate repo on /pause; warn on empty INGEST_SECRET
internal/webhook/handler.go           # Modified: add panic recovery to goroutines; require X-GitHub-Delivery; populate structural validator allowlists; reduce body size limit
internal/comment/sanitize.go          # Modified: add GFM image stripping
internal/comment/sanitize_test.go     # Modified: add test for image stripping
internal/safety/structural_test.go    # Modified: add test for nil-allowlist behaviour
internal/mirror/mirror.go             # Modified: validate sourceRepo format
internal/mirror/mirror_test.go        # Modified: add test for repo validation
.github/workflows/seed.yml            # Modified: add permissions block
```

---

## Task 1: Authenticate /cleanup and /health-check Endpoints

**Files:**
- Modify: `cmd/server/main.go`

The `/cleanup` and `/health-check` POST endpoints are publicly accessible with no auth. An attacker can close all shadow issues or spam health alert issues. Both should require `INGEST_SECRET` like `/ingest`, `/synthesize`, `/pause`, and `/unpause` already do.

Also add a startup warning when `INGEST_SECRET` is empty, matching the existing `GEMINI_API_KEY` warning pattern.

- [ ] **Step 1: Add auth check to /cleanup handler**

In `cmd/server/main.go`, add after the method check in the `/cleanup` handler (after line 142):

```go
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
```

- [ ] **Step 2: Add auth check to /health-check handler**

In `cmd/server/main.go`, add after the method check in the `/health-check` handler (after line 195):

```go
		if !validateIngestAuth(r.Header.Get("Authorization"), ingestSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
```

- [ ] **Step 3: Add startup warning for empty INGEST_SECRET**

In `cmd/server/main.go`, after `ingestSecret := os.Getenv("INGEST_SECRET")` (line 51), add:

```go
	if ingestSecret == "" {
		logger.Warn("INGEST_SECRET not set — /cleanup, /health-check, /ingest, /synthesize, /pause, /unpause are unauthenticated")
	}
```

- [ ] **Step 4: Add repo validation to /pause and /unpause**

In both `/pause` (after line 311) and `/unpause` (after line 338) handlers, add after the `repo == ""` check:

```go
		if !allowedRepos[repo] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/server/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "security: authenticate /cleanup and /health-check, validate /pause repo

Add INGEST_SECRET auth to /cleanup and /health-check endpoints that
were previously unauthenticated. Validate repo parameter against
allowedRepos on /pause and /unpause. Warn at startup if INGEST_SECRET
is empty."
```

---

## Task 2: Add Panic Recovery to Background Goroutines

**Files:**
- Modify: `internal/webhook/handler.go`

Background goroutines spawned for issue processing, comment processing, and push events have no `recover()`. A panic in LLM JSON parsing or type assertions crashes the entire server process.

- [ ] **Step 1: Add recoverGoroutine helper**

Add to `internal/webhook/handler.go` after the imports:

```go
// recoverGoroutine logs panics in background goroutines instead of crashing.
func recoverGoroutine(logger *slog.Logger, name string) {
	if r := recover(); r != nil {
		logger.Error("panic in background goroutine", "goroutine", name, "error", r)
	}
}
```

- [ ] **Step 2: Add defer recover to issues goroutine**

In the `case "issues":` block (around line 131-137), add `defer recoverGoroutine(h.logger, "processEvent")` as the first defer after `h.wg.Done()`:

```go
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer recoverGoroutine(h.logger, "processEvent")
			ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
			defer cancel()
			h.processEvent(ctx, event)
		}()
```

- [ ] **Step 3: Add defer recover to issue_comment goroutine**

Same pattern in the `case "issue_comment":` block (around line 147-157):

```go
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer recoverGoroutine(h.logger, "processCommentEvent")
			ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
			defer cancel()
			h.processCommentEvent(ctx, event)
		}()
```

- [ ] **Step 4: Add defer recover to push goroutine**

Same pattern in the `case "push":` block (around line 162-178):

```go
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer recoverGoroutine(h.logger, "handlePush")
			ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
			defer cancel()
			h.handlePush(ctx, event)
		}()
```

- [ ] **Step 5: Require X-GitHub-Delivery header**

Change the delivery ID check (around line 106-120) to reject requests without the header:

```go
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		http.Error(w, "missing X-GitHub-Delivery header", http.StatusBadRequest)
		return
	}
	duplicate, err := h.store.CheckAndRecordDelivery(r.Context(), deliveryID)
	if err != nil {
		h.logger.Error("checking delivery ID", "error", err)
		http.Error(w, "dedup check failed", http.StatusInternalServerError)
		return
	}
	if duplicate {
		h.logger.Info("duplicate delivery rejected", "deliveryID", deliveryID)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "duplicate delivery")
		return
	}
```

- [ ] **Step 6: Reduce webhook body size limit**

Change the constant at line 28 from 25 MB to 2 MB:

```go
const (
	maxWebhookBodySize = 2 << 20 // 2 MB — issue/comment events are <100 KB; push events rarely exceed 1 MB
	maxCommentLength   = 65536
	triageTimeout      = 5 * time.Minute
)
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/webhook/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/webhook/handler.go
git commit -m "security: add panic recovery, require delivery ID, reduce body limit

Add defer/recover to all background goroutines to prevent panics from
crashing the server. Require X-GitHub-Delivery header (always present
in legitimate webhooks). Reduce body size limit from 25 MB to 2 MB."
```

---

## Task 3: Fix Structural Validator Allowlists and GFM Image Injection

**Files:**
- Modify: `internal/webhook/handler.go` — populate allowlists when creating StructuralValidator
- Modify: `internal/comment/sanitize.go` — add GFM image stripping
- Modify: `internal/comment/sanitize_test.go` — add image stripping test
- Modify: `internal/safety/structural_test.go` — add test confirming allowlists are active

The structural validator is instantiated with nil `AllowedMentions` and `AllowedURLHosts`, which causes the mention and URL checks to be skipped entirely. Also, `sanitizeLLMOutput` doesn't strip GFM image syntax (`![](url)`) which can be used for pixel tracking.

- [ ] **Step 1: Add GFM image stripping test**

Add to `internal/comment/sanitize_test.go`:

```go
func TestSanitizeLLMOutputStripsGFMImages(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tracking pixel",
			input: "Try this fix: ![](https://tracker.example.com/pixel)",
			want:  "Try this fix: ",
		},
		{
			name:  "image with alt text",
			input: "See ![screenshot](https://evil.com/img.png) for details",
			want:  "See  for details",
		},
		{
			name:  "regular markdown link preserved",
			input: "See [this guide](https://github.com/docs) for details",
			want:  "See [this guide](https://github.com/docs) for details",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLLMOutput(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLLMOutput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/comment/ -run TestSanitizeLLMOutputStripsGFMImages -v`
Expected: FAIL — images not stripped yet.

- [ ] **Step 3: Implement GFM image stripping**

In `internal/comment/sanitize.go`, add a new regex and apply it in `sanitizeLLMOutput`:

```go
var (
	dangerousLinkRe  = regexp.MustCompile(`\[(.*?)\]\((?i:javascript|data|vbscript):.*\)`)
	scriptTagRe      = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlTagRe        = regexp.MustCompile(`<[^>]*>`)
	dangerousSchemeRe = regexp.MustCompile(`(?i)^(javascript|data|vbscript):`)
	gfmImageRe       = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
)

func sanitizeLLMOutput(s string) string {
	s = gfmImageRe.ReplaceAllString(s, "")
	s = dangerousLinkRe.ReplaceAllString(s, "[$1](removed)")
	s = scriptTagRe.ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/comment/ -run TestSanitizeLLMOutputStripsGFMImages -v`
Expected: PASS

- [ ] **Step 5: Populate structural validator allowlists**

In `internal/webhook/handler.go`, change the `StructuralValidator` construction (around line 56-58):

```go
	structural := safety.NewStructuralValidator(safety.StructuralConfig{
		MaxCommentLength: maxCommentLength,
		AllowedURLHosts: []string{
			"github.com",
			"ismaelmartinez.github.io",
			"teams.microsoft.com",
			"feedbackportal.microsoft.com",
			"learn.microsoft.com",
			"electronjs.org",
			"www.electronjs.org",
			"releases.electronjs.org",
		},
		AllowedMentions: []string{}, // empty = block all mentions in LLM output
	})
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/comment/ ./internal/safety/ ./internal/webhook/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/comment/sanitize.go internal/comment/sanitize_test.go internal/webhook/handler.go
git commit -m "security: activate URL/mention allowlists, strip GFM images

Populate StructuralValidator with URL host allowlist and empty mention
list (blocks all @mentions in LLM output). Add GFM image regex to
sanitizeLLMOutput to prevent tracking pixel injection via ![](url)."
```

---

## Task 4: Validate Mirror Source Repo Format

**Files:**
- Modify: `internal/mirror/mirror.go`
- Modify: `internal/mirror/mirror_test.go`

The `sourceRepo` parameter flows from webhook data into git URLs without format validation. While `exec.Command` prevents shell injection, a malformed repo slug could produce unexpected git behaviour.

- [ ] **Step 1: Add repo format validation test**

Add to `internal/mirror/mirror_test.go`:

```go
func TestValidateRepoFormat(t *testing.T) {
	tests := []struct {
		repo  string
		valid bool
	}{
		{"owner/repo", true},
		{"IsmaelMartinez/teams-for-linux", true},
		{"owner/repo-name.v2", true},
		{"owner/repo_name", true},
		{"", false},
		{"noslash", false},
		{"../../etc/passwd", false},
		{"owner/repo;rm -rf /", false},
		{"owner/repo\ninjection", false},
		{"owner/repo@attacker.com", false},
		{"a/b/c", false},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got := validRepoSlug(tt.repo)
			if got != tt.valid {
				t.Errorf("validRepoSlug(%q) = %v, want %v", tt.repo, got, tt.valid)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mirror/ -run TestValidateRepoFormat -v`
Expected: FAIL — `validRepoSlug` not defined.

- [ ] **Step 3: Implement validRepoSlug**

Add to `internal/mirror/mirror.go`:

```go
var repoSlugRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

// validRepoSlug checks that a repo string matches the expected owner/name format.
func validRepoSlug(repo string) bool {
	return repoSlugRe.MatchString(repo)
}
```

Add `"regexp"` to the imports.

- [ ] **Step 4: Add validation to Sync**

In `mirror.go`, add at the start of the `Sync` method (after the lock acquisition):

```go
	if !validRepoSlug(sourceRepo) || !validRepoSlug(shadowRepo) {
		return fmt.Errorf("invalid repo slug format: source=%q shadow=%q", sourceRepo, shadowRepo)
	}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/mirror/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mirror/mirror.go internal/mirror/mirror_test.go
git commit -m "security: validate repo slug format before git URL construction

Add regex validation for owner/name format on sourceRepo and shadowRepo
before they are interpolated into git URLs. Prevents malformed repo
slugs from webhook data causing unexpected git behaviour."
```

---

## Task 5: Harden CI Workflows

**Files:**
- Modify: `.github/workflows/seed.yml`

The `seed.yml` workflow has no `permissions:` block, inheriting broad defaults.

- [ ] **Step 1: Add permissions block to seed.yml**

Add after the `on:` block and before `jobs:`:

```yaml
permissions:
  contents: read
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/seed.yml
git commit -m "security: add minimal permissions to seed.yml workflow

Restricts seed workflow to contents:read instead of inheriting default
write permissions."
```

---

## Task 6: Update SECURITY.md and Run Full Verification

**Files:**
- Modify: `SECURITY.md`

Update the security policy to document the fixes applied.

- [ ] **Step 1: Update SECURITY.md**

Add a section after "### Code Execution":

```markdown
### Endpoint Authentication

All mutating endpoints (`/cleanup`, `/health-check`, `/ingest`, `/synthesize`, `/pause`, `/unpause`) require a Bearer token matching the `INGEST_SECRET` environment variable. The service logs a warning at startup if this secret is not set. Read-only endpoints (`/report`, `/report/trends`, `/dashboard`) serve aggregated data and are publicly accessible. The `/api/triage/` and `/api/agent/` detail endpoints return per-issue data scoped to configured repositories.

### Webhook Security

Webhook payloads require both a valid HMAC-SHA256 signature (`X-Hub-Signature-256`) and a unique delivery ID (`X-GitHub-Delivery`). Payloads without a delivery ID are rejected. Replay protection tracks delivery IDs for 30 days. The webhook body size is limited to 2 MB. Background processing goroutines include panic recovery to prevent a single malformed event from crashing the server.

### Output Sanitisation

LLM-generated output passes through `sanitizeLLMOutput` which strips HTML tags, script elements, dangerous URI schemes, and GFM image syntax (preventing tracking pixel injection). The structural validator enforces a URL hostname allowlist and blocks all @mentions in LLM output. Both layers run before content reaches any GitHub comment.
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: all PASS

- [ ] **Step 3: Run vet and linter**

Run: `go vet ./... && $(go env GOPATH)/bin/golangci-lint run ./...`
Expected: clean

- [ ] **Step 4: Commit**

```bash
git add SECURITY.md
git commit -m "docs: update SECURITY.md with hardening details

Document endpoint authentication, webhook security (delivery ID
enforcement, body size limit, panic recovery), and output sanitisation
(GFM image stripping, URL allowlist, mention blocking)."
```
