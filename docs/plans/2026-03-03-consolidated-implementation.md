# Consolidated Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Harden the triage bot's security, seed the full issue dataset, add a public dashboard, convert to a GitHub App, and cut over to production on teams-for-linux — validating everything iteratively on triage-bot-test-repo.

**Architecture:** Nine batches (0-8) executed sequentially. Each batch deploys to Cloud Run and validates on the test repo. Security fixes come first (Batches 1-4), then data seeding (Batch 5), dashboard (Batch 6), GitHub App (Batch 7), and production cutover (Batch 8).

**Tech Stack:** Go 1.26, Terraform >= 1.5, GCS, Google Secret Manager, GitHub Actions, Gemini 2.5 Flash + gemini-embedding-001, Neon PostgreSQL + pgvector, Cloud Run v2, bradleyfalzon/ghinstallation.

---

## Batch 0: Baseline Validation

### Task 1: Create baseline test issues on triage-bot-test-repo

This is an operational task — no code changes. Create 5 test issues that exercise each phase, document the bot's responses.

**Step 1: Create a bug report with missing sections (Phase 1)**

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "App crashes on startup after update" \
  --body "After updating to the latest version, the app crashes immediately on startup." \
  --label "bug"
```

Wait 15 seconds for the bot to respond, then record the comment:
```bash
gh issue view 1 --repo IsmaelMartinez/triage-bot-test-repo --comments
```

Expected: Phase 1 detects missing Reproduction steps, Expected Behavior, Debug, and PWA reproducibility sections.

**Step 2: Create a bug matching troubleshooting docs (Phase 2)**

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "Screen sharing shows black window" \
  --body "When I share my screen, the other participants see a completely black window. I'm using Wayland on Fedora 41 with GNOME. The screen sharing picker appears but after selecting a window, it's just black.\n\n### Reproduction steps\n1. Start a call\n2. Click share screen\n3. Select a window\n4. Other participants see black\n\n### Expected Behavior\nParticipants should see my screen content.\n\n### Debug\nNo errors in console.\n\n### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\nNo" \
  --label "bug"
```

Wait 15 seconds, record the comment. Expected: Phase 2 finds screen sharing troubleshooting docs.

**Step 3: Create a duplicate bug (Phase 3)**

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "Notifications not working on Linux" \
  --body "I'm not receiving any desktop notifications from Teams for Linux. I've checked my notification settings and they are enabled.\n\n### Reproduction steps\n1. Receive a message in Teams\n2. No notification appears\n\n### Expected Behavior\nDesktop notification should appear.\n\n### Debug\nNone.\n\n### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\nNo" \
  --label "bug"
```

Wait 15 seconds, record the comment. Expected: Phase 3 finds similar notification-related issues from the 111 seeded issues.

**Step 4: Create an enhancement request (Phase 4a)**

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "Add support for custom themes" \
  --body "It would be great if Teams for Linux could support custom CSS themes, allowing users to customize the appearance beyond just dark/light mode." \
  --label "enhancement"
```

Wait 15 seconds, record the comment. Expected: Phase 4a finds relevant roadmap/ADR/research matches if any exist in the feature index.

**Step 5: Create a misclassified issue (Phase 4b)**

Run:
```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "How do I change the notification sound?" \
  --body "I want to change the notification sound in Teams for Linux. Is this possible? Where can I find this setting?" \
  --label "bug"
```

Wait 15 seconds, record the comment. Expected: Phase 4b detects this is a question, not a bug.

**Step 6: Save baseline results**

Create a file documenting all bot responses:
```bash
mkdir -p docs/validation
```

Create `docs/validation/batch0-baseline.md` with each issue number, its title, the bot's full comment, and which phases fired. This is the reference for regression testing.

**Step 7: Commit**

```bash
git add docs/validation/batch0-baseline.md
git commit -m "docs: add baseline validation results from test repo"
```

---

## Batch 1: Quick Security Fixes

### Task 2: Move Gemini API key from URL to request header

**Files:**
- Modify: `internal/llm/client.go:49,94`
- Test: `go test ./internal/llm/...`

**Step 1: Update GenerateJSON to use header auth**

In `internal/llm/client.go`, change line 49 from:
```go
url := fmt.Sprintf("%s/models/gemini-2.5-flash:generateContent?key=%s", c.baseURL, c.apiKey)
```
to:
```go
url := fmt.Sprintf("%s/models/gemini-2.5-flash:generateContent", c.baseURL)
```

Then add the header after line 54 (after `req.Header.Set("Content-Type", "application/json")`):
```go
req.Header.Set("x-goog-api-key", c.apiKey)
```

**Step 2: Update Embed to use header auth**

Change line 94 from:
```go
url := fmt.Sprintf("%s/models/gemini-embedding-001:embedContent?key=%s", c.baseURL, c.apiKey)
```
to:
```go
url := fmt.Sprintf("%s/models/gemini-embedding-001:embedContent", c.baseURL)
```

Then add the header after line 99 (after `req.Header.Set("Content-Type", "application/json")`):
```go
req.Header.Set("x-goog-api-key", c.apiKey)
```

**Step 3: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass (no existing LLM tests make real API calls).

**Step 4: Commit**

```bash
git add internal/llm/client.go
git commit -m "security: move Gemini API key from URL to request header"
```

---

### Task 3: Add request body size limit to webhook handler

**Files:**
- Modify: `internal/webhook/handler.go:49`

**Step 1: Add MaxBytesReader before io.ReadAll**

In `internal/webhook/handler.go`, replace line 49:
```go
body, err := io.ReadAll(r.Body)
```
with:
```go
r.Body = http.MaxBytesReader(w, r.Body, 25<<20) // 25 MB
body, err := io.ReadAll(r.Body)
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/webhook/handler.go
git commit -m "security: add 25 MB body size limit to webhook handler"
```

---

### Task 4: Run Docker container as non-root

**Files:**
- Modify: `Dockerfile:14`

**Step 1: Add non-root user**

In `Dockerfile`, after line 14 (`RUN apk add --no-cache ca-certificates tzdata`), add:
```dockerfile
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
```

Then before line 20 (`ENTRYPOINT ["server"]`), add:
```dockerfile
USER appuser
```

The full runtime stage becomes:
```dockerfile
# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
COPY --from=builder /server /usr/local/bin/server
COPY --from=builder /seed /usr/local/bin/seed
COPY migrations /migrations

USER appuser
EXPOSE 8080
ENTRYPOINT ["server"]
```

**Step 2: Build locally to verify**

Run: `docker build --platform linux/amd64 -t triage-bot-test .`
Expected: Build succeeds.

**Step 3: Commit**

```bash
git add Dockerfile
git commit -m "security: run Docker container as non-root user"
```

---

### Task 5: Cap error response body reads

**Files:**
- Modify: `internal/llm/client.go:63,108`
- Modify: `internal/github/client.go:58,88`

**Step 1: Cap LLM client error reads**

In `internal/llm/client.go`, change line 63:
```go
respBody, _ := io.ReadAll(resp.Body)
```
to:
```go
respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
```

Do the same at line 108 (the Embed error path).

**Step 2: Cap GitHub client error reads**

In `internal/github/client.go`, change line 58:
```go
respBody, _ := io.ReadAll(resp.Body)
```
to:
```go
respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
```

Do the same at line 88 (the ListComments error path).

**Step 3: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/llm/client.go internal/github/client.go
git commit -m "security: cap error response body reads to 4 KB"
```

---

### Task 6: Fix AfterConnect DDL hook

**Files:**
- Modify: `internal/store/postgres.go:163-167`

**Step 1: Replace DDL with pgvector.RegisterTypes**

In `internal/store/postgres.go`, replace lines 163-167:
```go
config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
    // Register pgvector type
    _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
    return err
}
```
with:
```go
config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
    return pgvector.RegisterTypes(ctx, conn)
}
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/store/postgres.go
git commit -m "fix: replace DDL AfterConnect hook with pgvector.RegisterTypes"
```

---

### Task 7: Add embedding dimension validation

**Files:**
- Modify: `internal/store/postgres.go:24,43`

**Step 1: Add constant and validation**

At the top of `internal/store/postgres.go`, after the `Store` struct (around line 21), add:
```go
const EmbeddingDim = 768
```

In `UpsertDocument` (line 24), add as the first line of the function:
```go
if len(doc.Embedding) != EmbeddingDim {
    return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(doc.Embedding), EmbeddingDim)
}
```

In `UpsertIssue` (line 43), add as the first line of the function:
```go
if len(issue.Embedding) != EmbeddingDim {
    return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(issue.Embedding), EmbeddingDim)
}
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/store/postgres.go
git commit -m "fix: validate embedding dimensions before database insert"
```

---

### Task 8: Clean up go.sum

**Step 1: Run go mod tidy**

Run: `go mod tidy`

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add go.sum
git commit -m "chore: clean stale entries from go.sum"
```

---

### Task 9: Validate Batch 1 on test repo

**Step 1: Push to main to trigger deploy**

```bash
git push
```

Wait for the CI/CD workflow to complete. Verify the deploy succeeded:
```bash
gh run list --repo IsmaelMartinez/github-issue-triage-bot --limit 1
```

**Step 2: Create a test issue**

```bash
gh issue create --repo IsmaelMartinez/triage-bot-test-repo \
  --title "[Batch 1 validation] Screen sharing broken on Wayland" \
  --body "Screen sharing shows black window on Wayland.\n\n### Reproduction steps\n1. Share screen\n\n### Expected Behavior\nScreen visible to others.\n\n### Debug\nNone.\n\n### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\nNo" \
  --label "bug"
```

**Step 3: Verify bot responds correctly**

Wait 15 seconds, then check the comment. Compare against Batch 0 baseline — should be functionally equivalent.

**Step 4: Document results**

Update `docs/validation/batch1-security-fixes.md` with the test issue number and bot response.

**Step 5: Commit**

```bash
git add docs/validation/batch1-security-fixes.md
git commit -m "docs: add Batch 1 validation results"
```

---

## Batch 2: Secret Manager Migration

### Task 10: Create Secret Manager resources in Terraform

**Files:**
- Modify: `terraform/main.tf`

**Step 1: Enable the Secret Manager API**

Add after the existing `google_project_service` resources (around line 114):
```hcl
resource "google_project_service" "secretmanager" {
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}
```

**Step 2: Create a service account for Cloud Run**

Add after the API resources:
```hcl
resource "google_service_account" "triage_bot" {
  account_id   = "triage-bot-run"
  display_name = "Triage Bot Cloud Run"
}
```

**Step 3: Create secrets and versions**

Add Secret Manager resources for each of the four secrets:
```hcl
resource "google_secret_manager_secret" "database_url" {
  secret_id = "triage-bot-database-url"
  replication { auto {} }
  depends_on = [google_project_service.secretmanager]
}

resource "google_secret_manager_secret_version" "database_url" {
  secret      = google_secret_manager_secret.database_url.id
  secret_data = var.database_url
}

resource "google_secret_manager_secret_iam_member" "database_url_access" {
  secret_id = google_secret_manager_secret.database_url.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.triage_bot.email}"
}
```

Repeat the same three-resource pattern for `gemini_api_key`, `github_token`, and `webhook_secret`.

**Step 4: Update Cloud Run to use the service account and secret references**

In the `google_cloud_run_v2_service` resource, add the service account to the template:
```hcl
template {
    service_account = google_service_account.triage_bot.email
    containers {
```

Replace the four plaintext `env` blocks (lines 142-157) with secret references:
```hcl
env {
    name = "DATABASE_URL"
    value_source {
        secret_key_ref {
            secret  = google_secret_manager_secret.database_url.secret_id
            version = "latest"
        }
    }
}
env {
    name = "GEMINI_API_KEY"
    value_source {
        secret_key_ref {
            secret  = google_secret_manager_secret.gemini_api_key.secret_id
            version = "latest"
        }
    }
}
env {
    name = "GITHUB_TOKEN"
    value_source {
        secret_key_ref {
            secret  = google_secret_manager_secret.github_token.secret_id
            version = "latest"
        }
    }
}
env {
    name = "WEBHOOK_SECRET"
    value_source {
        secret_key_ref {
            secret  = google_secret_manager_secret.webhook_secret.secret_id
            version = "latest"
        }
    }
}
```

Keep the `SOURCE_REPO` env var as plaintext (it's not a secret).

**Step 5: Remove billing_account_id default**

Change line 60 from `default = "01B3C7-DE2DE2-BB9ACE"` to no default — move the value to terraform.tfvars.

**Step 6: Plan and apply**

Run:
```bash
cd terraform && terraform plan
```
Review the plan — it should create 13 new resources (1 API, 1 SA, 4 secrets, 4 versions, 4 IAM bindings) and update the Cloud Run service. Then:
```bash
terraform apply
```

**Step 7: Validate on test repo**

Create a test issue on triage-bot-test-repo and verify the bot still responds.

**Step 8: Commit**

```bash
git add terraform/main.tf
git commit -m "security: migrate Cloud Run secrets to Secret Manager"
```

---

## Batch 3: Prompt Injection Defenses

### Task 11: Add systemInstruction support to Gemini client

**Files:**
- Modify: `internal/llm/client.go`

**Step 1: Add system instruction types**

Add a new request type alongside the existing `geminiRequest` struct (around line 122):
```go
type geminiRequestWithSystem struct {
    SystemInstruction *content         `json:"systemInstruction,omitempty"`
    Contents          []content        `json:"contents"`
    GenerationConfig  generationConfig `json:"generationConfig"`
}
```

**Step 2: Add GenerateJSONWithSystem method**

Add a new method after `GenerateJSON`:
```go
// GenerateJSONWithSystem sends a system prompt + user content to Gemini.
// The system prompt contains trusted instructions; user content contains untrusted input.
func (c *Client) GenerateJSONWithSystem(ctx context.Context, systemPrompt, userContent string, temperature float64, maxTokens int) (string, error) {
    body := geminiRequestWithSystem{
        SystemInstruction: &content{Parts: []part{{Text: systemPrompt}}},
        Contents: []content{
            {Parts: []part{{Text: userContent}}},
        },
        GenerationConfig: generationConfig{
            Temperature:      temperature,
            MaxOutputTokens:  maxTokens,
            ResponseMimeType: "application/json",
        },
    }

    raw, err := json.Marshal(body)
    if err != nil {
        return "", fmt.Errorf("marshal request: %w", err)
    }

    url := fmt.Sprintf("%s/models/gemini-2.5-flash:generateContent", c.baseURL)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
    if err != nil {
        return "", fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-goog-api-key", c.apiKey)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("send request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(respBody))
    }

    var result geminiResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", fmt.Errorf("decode response: %w", err)
    }

    if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
        return "", fmt.Errorf("empty response from gemini")
    }

    return result.Candidates[0].Content.Parts[0].Text, nil
}
```

**Step 3: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/llm/client.go
git commit -m "feat: add GenerateJSONWithSystem for prompt injection defense"
```

---

### Task 12: Migrate Phase 2-4b to use system instructions

**Files:**
- Modify: `internal/phases/phase2.go:43-58`
- Modify: `internal/phases/phase3.go:40-57`
- Modify: `internal/phases/phase4a.go:42-62`
- Modify: `internal/phases/phase4b.go:16-37`

**Step 1: Update Phase 2**

In `internal/phases/phase2.go`, split the prompt at line 43. The system instruction is everything up to and including the JSON format instructions. The user content is the issue data and context documents.

Replace the single `prompt` variable and `l.GenerateJSON` call with:

```go
systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Match the bug report against known issues from our documentation.

Return a JSON array of 0-3 matches. Only include sections with a meaningful connection (shared symptoms, similar error, same component). Use humble language in the reason field ("appears similar", "might be related", "could be connected").

Format: [{"index": 0, "reason": "This appears similar because...", "actionable_step": "Try clearing the cache..."}]

If no sections match, return: []
Respond with ONLY valid JSON, no other text.`

userContent := fmt.Sprintf("KNOWN ISSUES:\n%s\n\nBUG REPORT:\nTitle: %s\nBody: %s",
    strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.3, 8192)
```

**Step 2: Update Phase 3**

In `internal/phases/phase3.go`, apply the same split. System instruction gets the role, rules, and format. User content gets the existing issues and new issue:

```go
systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Compare the new issue against existing issues to find potential duplicates or closely related reports.

Return a JSON array of 0-3 matches. Only include issues with a strong semantic connection (same bug, same feature request, clearly overlapping symptoms). Use humble language in the reason field ("might be related", "appears similar", "could be the same issue").

For each match, estimate a similarity percentage (60-95). Only include matches above 60%.

Format: [{"number": 123, "reason": "This might be related because...", "similarity": 75}]

If no issues are similar, return: []
Respond with ONLY valid JSON, no other text.`

userContent := fmt.Sprintf("EXISTING ISSUES:\n%s\n\nNEW ISSUE:\nTitle: %s\nBody: %s",
    strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.2, 8192)
```

**Step 3: Update Phase 4a**

In `internal/phases/phase4a.go`, same pattern:

```go
systemPrompt := `You are a helpful assistant for the "Teams for Linux" open source project.
Match this enhancement request against our existing roadmap items, architecture decisions (ADRs), and research documents.

Return a JSON array of 0-3 matches. Only include items with a meaningful connection to the enhancement request (same feature area, overlapping goals, related technical decisions).

For each match, include:
- "index": the item index number
- "reason": a brief explanation using humble language ("appears related", "might be connected", "could be relevant")
- "is_infeasible": true ONLY if the matched item has status "rejected" and the rejection reason clearly applies to this request. false otherwise.

Format: [{"index": 0, "reason": "We've previously investigated this area...", "is_infeasible": false}]

If no items match, return: []
Respond with ONLY valid JSON, no other text.`

userContent := fmt.Sprintf("EXISTING FEATURES/DECISIONS/RESEARCH:\n%s\n\nENHANCEMENT REQUEST:\nTitle: %s\nBody: %s",
    strings.Join(summaries, "\n"), truncate(title, 200), cleanBody)

raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.3, 8192)
```

**Step 4: Update Phase 4b**

In `internal/phases/phase4b.go`:

```go
systemPrompt := `You are a classification assistant for the "Teams for Linux" open source project.

Classify the GitHub issue as one of: bug, enhancement, or question.

Classification rules:
- "bug": Something that used to work and broke, a crash, an error, unexpected behavior, a regression
- "enhancement": A new feature request, an improvement to existing functionality, a UI change suggestion
- "question": A how-to question, a request for help or documentation

Return a JSON object with:
- "classification": one of "bug", "enhancement", "question"
- "confidence": a number from 0-100 indicating how confident you are
- "reason": a brief explanation (1 sentence) of why you chose this classification

Format: {"classification": "bug", "confidence": 85, "reason": "The issue describes something that stopped working after an update."}
Respond with ONLY valid JSON, no other text.`

userContent := fmt.Sprintf("ISSUE:\nTitle: %s\nBody: %s\n\nCurrent label: %s",
    truncate(title, 200), cleanBody, currentLabel)

raw, err := l.GenerateJSONWithSystem(ctx, systemPrompt, userContent, 0.15, 8192)
```

**Step 5: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add internal/phases/phase2.go internal/phases/phase3.go internal/phases/phase4a.go internal/phases/phase4b.go
git commit -m "security: separate system instructions from user content in LLM prompts"
```

---

### Task 13: Add LLM output sanitization to comment builder

**Files:**
- Modify: `internal/comment/builder.go`
- Create: `internal/comment/sanitize.go`
- Create: `internal/comment/sanitize_test.go`

**Step 1: Write the sanitize test**

Create `internal/comment/sanitize_test.go`:
```go
package comment

import "testing"

func TestSanitizeLLMOutput(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"plain text", "This is normal text", "This is normal text"},
        {"strips javascript links", "[click](javascript:alert(1))", "[click](removed)"},
        {"strips data links", "[x](data:text/html,<script>)", "[x](removed)"},
        {"keeps safe links", "[docs](https://example.com)", "[docs](https://example.com)"},
        {"strips html tags", "text <script>alert(1)</script> more", "text  more"},
        {"keeps normal markdown", "**bold** and `code`", "**bold** and `code`"},
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

**Step 2: Run test to verify it fails**

Run: `go test ./internal/comment/ -run TestSanitizeLLMOutput -v`
Expected: FAIL — `sanitizeLLMOutput` not defined.

**Step 3: Write the sanitizer**

Create `internal/comment/sanitize.go`:
```go
package comment

import (
    "regexp"
    "strings"
)

var (
    dangerousLinkRe = regexp.MustCompile(`\[([^\]]*)\]\((javascript|data|vbscript):[^)]*\)`)
    htmlTagRe       = regexp.MustCompile(`<[^>]*>`)
)

// sanitizeLLMOutput removes dangerous patterns from LLM-generated text
// before it's included in GitHub comments.
func sanitizeLLMOutput(s string) string {
    // Replace dangerous link protocols with "removed"
    s = dangerousLinkRe.ReplaceAllString(s, "[$1](removed)")
    // Strip HTML tags
    s = htmlTagRe.ReplaceAllString(s, "")
    return s
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/comment/ -run TestSanitizeLLMOutput -v`
Expected: PASS.

**Step 5: Apply sanitization in builder.go**

In `internal/comment/builder.go`, apply `sanitizeLLMOutput` to all LLM-generated fields. In the Phase 2 section (line 59), change:
```go
parts = append(parts, fmt.Sprintf("- [%s](%s) — %s %s\n", s.Title, s.DocURL, s.Reason, s.ActionableStep))
```
to:
```go
parts = append(parts, fmt.Sprintf("- [%s](%s) — %s %s\n", s.Title, s.DocURL, sanitizeLLMOutput(s.Reason), sanitizeLLMOutput(s.ActionableStep)))
```

In the Phase 3 section (line 74), change:
```go
parts = append(parts, fmt.Sprintf("- #%d — \"%s\" (%d%% similar) — %s", d.Number, d.Title, d.Similarity, d.Reason))
```
to:
```go
parts = append(parts, fmt.Sprintf("- #%d — \"%s\" (%d%% similar) — %s", d.Number, d.Title, d.Similarity, sanitizeLLMOutput(d.Reason)))
```

Apply the same to line 86 (closed duplicates), lines 124-128 (Phase 4a context matches), and line 174 (Phase 4b misclassification reason).

**Step 6: Run all tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 7: Commit**

```bash
git add internal/comment/sanitize.go internal/comment/sanitize_test.go internal/comment/builder.go
git commit -m "security: sanitize LLM output before posting GitHub comments"
```

---

### Task 14: Validate Batch 3 on test repo

Same pattern as Task 9. Push, wait for deploy, create a test issue, compare against baseline.

---

## Batch 4: Webhook Replay Protection and CI Hardening

### Task 15: Add webhook_deliveries table

**Files:**
- Create: `migrations/002_webhook_deliveries.sql`

**Step 1: Write the migration**

Create `migrations/002_webhook_deliveries.sql`:
```sql
-- Track webhook delivery IDs for replay protection.
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Clean up old entries (keep 7 days). Run via scheduled job or application logic.
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries (created_at);
```

**Step 2: Apply the migration**

Run against the Neon database:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -f migrations/002_webhook_deliveries.sql
```
Expected: Table and index created.

**Step 3: Commit**

```bash
git add migrations/002_webhook_deliveries.sql
git commit -m "db: add webhook_deliveries table for replay protection"
```

---

### Task 16: Add delivery ID checking to webhook handler

**Files:**
- Modify: `internal/store/postgres.go` (add CheckAndRecordDelivery method)
- Modify: `internal/webhook/handler.go` (check delivery ID before processing)

**Step 1: Add store method**

In `internal/store/postgres.go`, add:
```go
// CheckAndRecordDelivery atomically checks if a delivery ID has been seen and records it.
// Returns true if the delivery was already recorded (duplicate).
func (s *Store) CheckAndRecordDelivery(ctx context.Context, deliveryID string) (bool, error) {
    var exists bool
    err := s.pool.QueryRow(ctx, `
        WITH ins AS (
            INSERT INTO webhook_deliveries (delivery_id) VALUES ($1)
            ON CONFLICT (delivery_id) DO NOTHING
            RETURNING delivery_id
        )
        SELECT NOT EXISTS(SELECT 1 FROM ins)
    `, deliveryID).Scan(&exists)
    return exists, err
}
```

**Step 2: Check delivery ID in handler**

In `internal/webhook/handler.go`, after the signature verification (line 60) and before the event type check, add:
```go
// Check for replay attacks
deliveryID := r.Header.Get("X-GitHub-Delivery")
if deliveryID != "" {
    duplicate, err := h.store.CheckAndRecordDelivery(r.Context(), deliveryID)
    if err != nil {
        h.logger.Error("checking delivery ID", "error", err)
    } else if duplicate {
        h.logger.Info("duplicate delivery rejected", "deliveryID", deliveryID)
        w.WriteHeader(http.StatusOK)
        fmt.Fprint(w, "duplicate delivery")
        return
    }
}
```

**Step 3: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/store/postgres.go internal/webhook/handler.go
git commit -m "security: add webhook replay protection via delivery ID tracking"
```

---

### Task 17: Pin GitHub Actions to commit SHAs

**Files:**
- Modify: `.github/workflows/deploy.yml`

**Step 1: Look up current SHAs**

Look up the commit SHAs for each action version used in the workflow. Use GitHub's API or the action repos to find the exact SHA for each tag.

**Step 2: Replace tag references with SHAs**

In `.github/workflows/deploy.yml`, replace all `uses:` lines with SHA-pinned versions plus a version comment. For example:
```yaml
- uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4
- uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5  # v5
- uses: google-github-actions/auth@6fc4af4b145ae7821d527454aa9bd537d1f2dc5f  # v2
- uses: google-github-actions/setup-gcloud@6189d56e4096ee891640a46ee2f7e8d688e4e0e0  # v2
```

Note: The exact SHAs should be verified at implementation time since these may have been updated.

**Step 3: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "security: pin GitHub Actions to commit SHAs"
```

---

### Task 18: Validate Batch 4 on test repo

Push, deploy, create a test issue. Also test replay protection by re-delivering a webhook from the GitHub webhook settings page.

---

## Batch 5: Data Seeding

### Task 19: Create issue export tool

**Files:**
- Create: `cmd/export-issues/main.go`

**Step 1: Write the export tool**

Create `cmd/export-issues/main.go` — a CLI that uses the GitHub API to export all issues from a repo as JSON in the format the seed CLI expects. Key features: paginates through all issues, filters out pull requests, strips code fences and HTML from body, outputs JSON to stdout with progress on stderr. Uses a 500ms sleep between pages to respect rate limits.

See the code in the existing implementation plan (Task 2 in the old plan) for the full implementation.

**Step 2: Run the export**

```bash
GITHUB_TOKEN=$(grep github_token terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/export-issues > /tmp/all-issues.json
```
Expected: Outputs ~1,356 issues as JSON.

**Step 3: Commit**

```bash
git add cmd/export-issues/main.go
git commit -m "feat: add issue export tool for bulk seeding"
```

---

### Task 20: Add rate limiting to seed CLI

**Files:**
- Modify: `cmd/seed/main.go`

**Step 1: Add rate limiting**

In `cmd/seed/main.go`, add `"time"` to imports. In all three seed functions (`seedIssues`, `seedTroubleshooting`, `seedFeatures`), add a pause every 50 items:
```go
if (i+1)%50 == 0 {
    logger.Info("rate limit pause", "processed", i+1, "remaining", len(entries)-i-1)
    time.Sleep(3 * time.Second)
}
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add cmd/seed/main.go
git commit -m "feat: add rate limiting to seed CLI"
```

---

### Task 21: Seed all issues and feature index

**Step 1: Seed all issues**

```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
GEMINI_API_KEY=$(grep gemini_api_key terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/seed issues /tmp/all-issues.json
```
Expected: All ~1,356 issues seeded.

**Step 2: Seed the feature index**

Generate the feature index from the teams-for-linux docs-site, then seed it:
```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
GEMINI_API_KEY=$(grep gemini_api_key terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/seed features /path/to/feature-index.json
```

**Step 3: Verify counts**

```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -c "SELECT count(*) FROM issues WHERE repo = 'IsmaelMartinez/teams-for-linux'; SELECT doc_type, count(*) FROM documents WHERE repo = 'IsmaelMartinez/teams-for-linux' GROUP BY doc_type;"
```

---

### Task 22: Update ivfflat indexes for larger dataset

**Files:**
- Create: `migrations/003_update_ivfflat_lists.sql`

**Step 1: Write the migration**

Create `migrations/003_update_ivfflat_lists.sql`:
```sql
-- Update ivfflat index lists for larger dataset.
-- sqrt(1356) ~ 37, round to 40 for headroom.
DROP INDEX IF EXISTS idx_issues_embedding;
CREATE INDEX idx_issues_embedding ON issues USING ivfflat (embedding vector_cosine_ops) WITH (lists = 40);

-- Documents table (~450 entries), sqrt(450) ~ 21, round to 25.
DROP INDEX IF EXISTS idx_documents_embedding;
CREATE INDEX idx_documents_embedding ON documents USING ivfflat (embedding vector_cosine_ops) WITH (lists = 25);
```

**Step 2: Apply**

```bash
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  psql "$DATABASE_URL" -f migrations/003_update_ivfflat_lists.sql
```

**Step 3: Commit**

```bash
git add migrations/003_update_ivfflat_lists.sql
git commit -m "db: update ivfflat lists for larger dataset"
```

---

### Task 23: Remove export tool and validate

**Step 1: Remove the one-time export tool**

```bash
rm -rf cmd/export-issues
git add -A cmd/export-issues
git commit -m "chore: remove export-issues tool (seeding complete)"
```

**Step 2: Validate on test repo**

Create test issues that should match old teams-for-linux issues. Verify Phase 3 finds relevant matches from the full 1,356-issue dataset.

---

## Batch 6: Dashboard

### Task 24: Add dashboard store methods

**Files:**
- Create: `internal/store/report.go`

**Step 1: Write the report store**

Create `internal/store/report.go` with `GetDashboardStats`, `UpdateReactions`, and `ListBotComments` methods. See the design doc for the full struct and query definitions.

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/store/report.go
git commit -m "feat: add dashboard stats query methods"
```

---

### Task 25: Add /report endpoint

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: Add the report handler**

In `cmd/server/main.go`, add after the health check handler (line 72). Add `"encoding/json"` to imports:
```go
mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
    repo := r.URL.Query().Get("repo")
    if repo == "" {
        repo = "IsmaelMartinez/teams-for-linux"
    }
    stats, err := s.GetDashboardStats(r.Context(), repo)
    if err != nil {
        http.Error(w, "failed to get stats", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    json.NewEncoder(w).Encode(stats)
})
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: add /report JSON endpoint for dashboard"
```

---

### Task 26: Create static dashboard generator

**Files:**
- Create: `cmd/dashboard/main.go`
- Create: `cmd/dashboard/template.html`

Write a Go CLI that connects to the database, fetches dashboard stats, renders a single-page HTML file using Go templates. The HTML should have embedded CSS, stat cards, phase breakdown table, and recent comments table. No external dependencies or chart libraries.

See the design doc for details.

**Step 1: Write the generator and template**

**Step 2: Test locally**

```bash
mkdir -p docs/dashboard
DATABASE_URL=$(grep database_url terraform/terraform.tfvars | cut -d'"' -f2) \
  go run ./cmd/dashboard docs/dashboard/index.html
open docs/dashboard/index.html
```

**Step 3: Commit**

```bash
git add cmd/dashboard/ docs/dashboard/
git commit -m "feat: add static dashboard HTML generator"
```

---

### Task 27: Create reaction sync tool

**Files:**
- Create: `cmd/sync-reactions/main.go`

Write a CLI that queries bot_comments for all comment IDs, calls the GitHub API for reactions on each, and updates thumbs_up/thumbs_down in the database.

**Step 1: Write the tool**

**Step 2: Test locally**

**Step 3: Commit**

```bash
git add cmd/sync-reactions/main.go
git commit -m "feat: add reaction sync tool"
```

---

### Task 28: Add dashboard generation workflow

**Files:**
- Create: `.github/workflows/dashboard.yml`

Write a GitHub Actions workflow that runs daily at 6am UTC: syncs reactions, generates the dashboard, and publishes to GitHub Pages.

**Step 1: Write the workflow**

**Step 2: Commit and verify**

```bash
git add .github/workflows/dashboard.yml
git commit -m "ci: add daily dashboard generation workflow"
```

---

### Task 29: Validate Batch 6

Push, deploy, verify `/report` endpoint returns JSON, generate dashboard locally, review the output.

---

## Batch 7: GitHub App Conversion

### Task 30: Register GitHub App and add ghinstallation dependency

Register a GitHub App via GitHub Settings with Issues read/write permission and issues event subscription. Set the webhook URL to the Cloud Run service URL.

```bash
go get github.com/bradleyfalzon/ghinstallation/v2
```

**Commit:**
```bash
git add go.mod go.sum
git commit -m "deps: add ghinstallation for GitHub App auth"
```

---

### Task 31: Refactor GitHub client for App authentication

**Files:**
- Modify: `internal/github/client.go`
- Modify: `internal/webhook/handler.go`
- Modify: `cmd/server/main.go`

Replace the PAT-based `Client` with one that uses installation access tokens via `ghinstallation`. The client needs the App ID, private key, and extracts the installation ID from each webhook payload to get a scoped token.

Key changes:
- `New()` takes appID and privateKey instead of a token
- Add `TokenForInstallation(installationID)` method
- Handler extracts `installation.id` from the webhook payload
- Server reads `GITHUB_APP_ID` and `GITHUB_PRIVATE_KEY` env vars instead of `GITHUB_TOKEN`

This is a larger refactor — implement and test thoroughly.

---

### Task 32: Store App private key in Secret Manager

**Files:**
- Modify: `terraform/main.tf`

Add a Secret Manager secret for the GitHub App private key, replacing the `GITHUB_TOKEN` secret with `GITHUB_PRIVATE_KEY` and adding `GITHUB_APP_ID` as a plain env var.

---

### Task 33: Validate GitHub App on test repo

Clean switch: remove the old PAT webhook from triage-bot-test-repo, install the GitHub App, create test issues, verify the bot responds through the App identity.

---

## Batch 8: Production Cutover

### Task 34: Install GitHub App on teams-for-linux

Install the app, create a test issue, verify the bot responds, close and delete the test issue.

### Task 35: Disable old bot workflows

In teams-for-linux, disable the old triage bot GitHub Actions workflows. Keep them for one week as fallback.

### Task 36: Update documentation

Update `docs/decisions/001-remaining-work.md`, `CLAUDE.md`, and `README.md` to reflect the completed migration.

```bash
git add docs/ CLAUDE.md README.md
git commit -m "docs: mark production cutover complete"
```
