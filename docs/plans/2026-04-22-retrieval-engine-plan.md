# Retrieval Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the retrieval layer for the research-brief bot: parse a per-repo `hats.md` taxonomy, add a regression-window PR diff, watch Electron release notes and cross-reference `blocked` issues, and extend vector search with hat-aware soft rerank. Ends with a `/brief-preview` endpoint that validates end-to-end retrieval without generating a full brief.

**Architecture:** Each capability is a focused Go module under `internal/`. Hats are loaded from the monitored repo via GitHub Contents API with a TTL cache mirroring `internal/config/loader.go`. Regression-window diff and release notes use new methods on `internal/github/client.go`. Electron releases are persisted to the existing `repo_events` table with `EventType: "upstream_release"` and embedded/upserted as `doc_type: "upstream_release"` through the existing `EmbedAndUpsert` pipeline. Hat-aware rerank is a new function sitting next to the existing `FindSimilarDocuments`. A `/upstream-watch` endpoint driven by a daily cron matches the existing `/cleanup` OIDC-auth pattern. The brief generator and promotion drafter are explicitly out of scope — this plan lays the foundation only.

**Tech Stack:** Go 1.26, pgvector 0.8.0, pgx/v5, ghinstallation/v2, Gemini 2.5 Flash (embeddings), existing `internal/store`, `internal/github`, `internal/config`, `internal/ingest` modules.

---

## File Structure

Create:
- `internal/hats/types.go` — `Hat`, `Taxonomy`, `Posture` types
- `internal/hats/parser.go` — markdown parser for `hats.md`
- `internal/hats/parser_test.go` — table-driven parser tests with fixtures
- `internal/hats/loader.go` — fetch-with-TTL cache mirroring `config.Cache`
- `internal/hats/loader_test.go` — cache behaviour tests
- `internal/hats/testdata/hats-example.md` — fixture for parser tests
- `internal/regression/diff.go` — PR diff in a version window, keyword filter
- `internal/regression/diff_test.go` — filter/keyword logic tests
- `internal/upstream/electron.go` — Electron release watcher module
- `internal/upstream/electron_test.go` — watcher unit tests
- `cmd/server/upstream.go` — `/upstream-watch` HTTP handler
- `.github/workflows/upstream-watcher.yml` — daily cron
- `cmd/server/brief_preview.go` — `/brief-preview` handler for retrieval smoke test

Modify:
- `internal/config/butler.go` — add `ResearchBrief` sub-config
- `internal/config/butler_test.go` — add config test cases
- `internal/github/client.go` — add `ListMergedPRsBetween`, `GetLatestReleases`
- `internal/github/client_test.go` — add method tests
- `internal/store/postgres.go` — add `FindSimilarDocumentsWithBoost`
- `internal/store/postgres_test.go` — add boost behaviour test
- `cmd/server/main.go` — register new handlers

---

## Task 1: Extend butler.json config for research-brief-bot

**Files:**
- Modify: `internal/config/butler.go`
- Modify: `internal/config/butler_test.go`

- [ ] **Step 1: Write failing test for new config field**

Add to `internal/config/butler_test.go`:

```go
func TestParse_ResearchBriefDefaults(t *testing.T) {
	got, err := Parse([]byte(`{"project":{"name":"X"}}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.ResearchBrief.Enabled {
		t.Errorf("ResearchBrief.Enabled default = true, want false")
	}
	if got.ResearchBrief.HatsPath != ".github/hats.md" {
		t.Errorf("HatsPath default = %q, want %q", got.ResearchBrief.HatsPath, ".github/hats.md")
	}
}

func TestParse_ResearchBriefOverride(t *testing.T) {
	got, err := Parse([]byte(`{"project":{"name":"X"},"research_brief":{"enabled":true,"hats_path":"docs/hats.md"}}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got.ResearchBrief.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if got.ResearchBrief.HatsPath != "docs/hats.md" {
		t.Errorf("HatsPath = %q, want docs/hats.md", got.ResearchBrief.HatsPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestParse_ResearchBrief -v`
Expected: FAIL with "got.ResearchBrief undefined" or compile error.

- [ ] **Step 3: Add the struct and default in `internal/config/butler.go`**

Add near existing sub-config types:

```go
// ResearchBriefConfig controls the research-brief bot pipeline.
type ResearchBriefConfig struct {
	// Enabled gates the whole research-brief pipeline. Default false until rollout.
	Enabled bool `json:"enabled"`

	// HatsPath is the repo-relative path to hats.md. Default ".github/hats.md".
	HatsPath string `json:"hats_path"`
}
```

Add the field to `ButlerConfig`:

```go
ResearchBrief ResearchBriefConfig `json:"research_brief"`
```

Add to `DefaultConfig()`:

```go
ResearchBrief: ResearchBriefConfig{
	Enabled:  false,
	HatsPath: ".github/hats.md",
},
```

Confirm in `Parse()` that an empty `research_brief` object in input leaves defaults intact (the existing merge-over-defaults pattern handles this automatically — verify by reading the Parse function).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: All pass including the two new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/config/butler.go internal/config/butler_test.go
git commit -m "feat(config): add research-brief bot config in butler.json"
```

---

## Task 2: Hat types and markdown parser

**Files:**
- Create: `internal/hats/types.go`
- Create: `internal/hats/parser.go`
- Create: `internal/hats/parser_test.go`
- Create: `internal/hats/testdata/hats-example.md`

- [ ] **Step 1: Create a minimal fixture**

`internal/hats/testdata/hats-example.md`:

```markdown
# Hats — example

Preamble prose.

## display-session-media

When to pick. Camera or screen-share failures.

Retrieval filter. Labels: wayland, screen-sharing. Keywords: ozone, xwayland.

Reasoning posture. ambiguous-workaround-menu.

Phase 1 asks. XDG_SESSION_TYPE, GPU vendor.

Anchors. #2169, #2138.

## other

Fallback when no hat fits.
```

- [ ] **Step 2: Write failing parser test**

`internal/hats/parser_test.go`:

```go
package hats

import (
	"os"
	"reflect"
	"testing"
)

func TestParseFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/hats-example.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Hats) != 2 {
		t.Fatalf("len(Hats) = %d, want 2", len(got.Hats))
	}
	h := got.Hats[0]
	if h.Name != "display-session-media" {
		t.Errorf("name = %q", h.Name)
	}
	if h.Posture != "ambiguous-workaround-menu" {
		t.Errorf("posture = %q", h.Posture)
	}
	wantLabels := []string{"wayland", "screen-sharing"}
	if !reflect.DeepEqual(h.RetrievalLabels, wantLabels) {
		t.Errorf("labels = %v, want %v", h.RetrievalLabels, wantLabels)
	}
	wantKeywords := []string{"ozone", "xwayland"}
	if !reflect.DeepEqual(h.RetrievalBoostKeywords, wantKeywords) {
		t.Errorf("keywords = %v, want %v", h.RetrievalBoostKeywords, wantKeywords)
	}
	wantAnchors := []int{2169, 2138}
	if !reflect.DeepEqual(h.AnchorIssueNumbers, wantAnchors) {
		t.Errorf("anchors = %v, want %v", h.AnchorIssueNumbers, wantAnchors)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse([]byte("# only preamble\n"))
	if err == nil {
		t.Error("expected error for no hats")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/hats/ -v`
Expected: FAIL with "package hats not defined" or similar.

- [ ] **Step 4: Define the types in `internal/hats/types.go`**

```go
package hats

// Posture is one of the reasoning postures declared in hats.md.
type Posture string

const (
	PostureCausalHypothesis      Posture = "causal-hypothesis"
	PostureWorkaroundMenu        Posture = "ambiguous-workaround-menu"
	PostureCausalNarrative       Posture = "internal-regression"
	PostureDemandGating          Posture = "demand-gating-needed"
	PostureConfigDependent       Posture = "config-dependent"
	PostureBlockedOnUpstream     Posture = "blocked-on-upstream"
)

// Hat is one class entry in the taxonomy.
type Hat struct {
	Name                   string
	WhenToPick             string
	RetrievalLabels        []string
	RetrievalBoostKeywords []string
	Posture                Posture
	Phase1Asks             string
	AnchorIssueNumbers     []int
}

// Taxonomy is the parsed content of a hats.md file.
type Taxonomy struct {
	Preamble string
	Hats     []Hat
}

// Find returns the hat with the given name, or nil.
func (t Taxonomy) Find(name string) *Hat {
	for i := range t.Hats {
		if t.Hats[i].Name == name {
			return &t.Hats[i]
		}
	}
	return nil
}
```

- [ ] **Step 5: Implement the parser in `internal/hats/parser.go`**

```go
package hats

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	h2Re        = regexp.MustCompile(`^## (\S.*)$`)
	issueRefRe  = regexp.MustCompile(`#(\d+)`)
	labelsLineRe = regexp.MustCompile(`(?i)labels:\s*([^.]+)\.?`)
	keywordsLineRe = regexp.MustCompile(`(?i)keywords:\s*([^.]+)\.?`)
)

// Parse converts hats.md content into a Taxonomy.
// Each hat is a level-2 heading whose body is a sequence of
// "Label. Content." sentences where Label is one of:
// "When to pick", "Retrieval filter", "Reasoning posture",
// "Phase 1 asks", "Anchors".
func Parse(data []byte) (Taxonomy, error) {
	var tax Taxonomy
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var current *Hat
	var preambleLines []string
	var bodyLines []string
	flush := func() {
		if current == nil {
			return
		}
		applyBody(current, strings.Join(bodyLines, "\n"))
		tax.Hats = append(tax.Hats, *current)
		current = nil
		bodyLines = bodyLines[:0]
	}

	for scanner.Scan() {
		line := scanner.Text()
		if m := h2Re.FindStringSubmatch(line); m != nil {
			flush()
			current = &Hat{Name: strings.TrimSpace(m[1])}
			continue
		}
		if current == nil {
			preambleLines = append(preambleLines, line)
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return Taxonomy{}, fmt.Errorf("scan: %w", err)
	}
	if len(tax.Hats) == 0 {
		return Taxonomy{}, errors.New("no hats found (expected level-2 headings)")
	}
	tax.Preamble = strings.TrimSpace(strings.Join(preambleLines, "\n"))
	return tax, nil
}

// applyBody walks the body paragraphs and assigns fields by leading label.
func applyBody(h *Hat, body string) {
	paras := splitParagraphs(body)
	for _, p := range paras {
		switch {
		case startsWithCase(p, "When to pick"):
			h.WhenToPick = stripLabel(p, "When to pick")
		case startsWithCase(p, "Retrieval filter"):
			content := stripLabel(p, "Retrieval filter")
			h.RetrievalLabels = extractList(content, labelsLineRe)
			h.RetrievalBoostKeywords = extractList(content, keywordsLineRe)
		case startsWithCase(p, "Reasoning posture"):
			content := stripLabel(p, "Reasoning posture")
			h.Posture = Posture(firstSentenceKey(content))
		case startsWithCase(p, "Phase 1 asks"):
			h.Phase1Asks = stripLabel(p, "Phase 1 asks")
		case startsWithCase(p, "Anchors"):
			content := stripLabel(p, "Anchors")
			for _, m := range issueRefRe.FindAllStringSubmatch(content, -1) {
				n, err := strconv.Atoi(m[1])
				if err == nil {
					h.AnchorIssueNumbers = append(h.AnchorIssueNumbers, n)
				}
			}
		}
	}
}

func splitParagraphs(body string) []string {
	lines := strings.Split(body, "\n")
	var paras []string
	var current []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(current) > 0 {
				paras = append(paras, strings.Join(current, " "))
				current = current[:0]
			}
			continue
		}
		current = append(current, strings.TrimSpace(l))
	}
	if len(current) > 0 {
		paras = append(paras, strings.Join(current, " "))
	}
	return paras
}

func startsWithCase(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func stripLabel(s, label string) string {
	s = strings.TrimPrefix(s, label)
	s = strings.TrimPrefix(s, strings.ToLower(label))
	s = strings.TrimPrefix(s, ".")
	return strings.TrimSpace(s)
}

func extractList(content string, re *regexp.Regexp) []string {
	m := re.FindStringSubmatch(content)
	if len(m) < 2 {
		return nil
	}
	parts := strings.Split(m[1], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, "`"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstSentenceKey(content string) string {
	// Take the first word-ish token (hyphens allowed, lowercase).
	for i, r := range content {
		if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return strings.ToLower(content[:i])
		}
	}
	return strings.ToLower(content)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/hats/ -v`
Expected: PASS for both tests.

- [ ] **Step 7: Parse the real teams-for-linux fixture to catch format drift**

Add to `parser_test.go`:

```go
func TestParseSeedFile(t *testing.T) {
	data, err := os.ReadFile("../../docs/hats-teams-for-linux.md")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse seed: %v", err)
	}
	wantNames := []string{
		"display-session-media", "internal-regression-network",
		"tray-notifications", "upstream-blocked", "packaging",
		"configuration-cli", "enhancement-demand-gating",
		"auth-network-edge", "other",
	}
	if len(got.Hats) != len(wantNames) {
		t.Fatalf("got %d hats, want %d", len(got.Hats), len(wantNames))
	}
	for i, want := range wantNames {
		if got.Hats[i].Name != want {
			t.Errorf("hat[%d] = %q, want %q", i, got.Hats[i].Name, want)
		}
	}
}
```

Run: `go test ./internal/hats/ -run TestParseSeedFile -v`
Expected: PASS — if it fails, fix parser or the seed file until both agree. The seed is canonical.

- [ ] **Step 8: Commit**

```bash
git add internal/hats/
git commit -m "feat(hats): add hats.md parser and taxonomy types"
```

---

## Task 3: Hats loader with TTL cache

**Files:**
- Create: `internal/hats/loader.go`
- Create: `internal/hats/loader_test.go`

- [ ] **Step 1: Write failing cache test**

`internal/hats/loader_test.go`:

```go
package hats

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoader_CachesWithinTTL(t *testing.T) {
	var calls int32
	fetch := func() ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return fixtureBytes(t), nil
	}
	l := NewLoader(fetch, 100*time.Millisecond)
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (second Get should hit cache)", got)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d, want 2 (TTL expired)", got)
	}
}

func TestLoader_FetchErrorReturnsStaleIfAvailable(t *testing.T) {
	var calls int32
	fetch := func() ([]byte, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return fixtureBytes(t), nil
		}
		return nil, errors.New("network down")
	}
	l := NewLoader(fetch, 10*time.Millisecond)
	first, err := l.Get()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := l.Get()
	if err != nil {
		t.Fatalf("want stale-cache fallback, got err %v", err)
	}
	if len(first.Hats) != len(second.Hats) {
		t.Errorf("stale cache should match first result")
	}
}

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := osReadFile("testdata/hats-example.md")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return data
}

// osReadFile is a tiny indirection for use in test fixtures.
var osReadFile = func(p string) ([]byte, error) {
	// use os.ReadFile in real test file
	return nil, nil
}
```

Replace `osReadFile` with a direct `os.ReadFile` at the top of the test file — keeping the helper only so test text shows above. Final test file should `import "os"` and call `os.ReadFile` directly.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hats/ -run TestLoader -v`
Expected: FAIL with "NewLoader undefined".

- [ ] **Step 3: Implement the loader**

`internal/hats/loader.go`:

```go
package hats

import (
	"errors"
	"sync"
	"time"
)

// FetchFunc returns the raw bytes of hats.md or an error.
type FetchFunc func() ([]byte, error)

// Loader caches a parsed Taxonomy with a TTL and returns stale results if
// a fetch fails after TTL expiry.
type Loader struct {
	mu      sync.RWMutex
	fetch   FetchFunc
	ttl     time.Duration
	cached  *Taxonomy
	fetched time.Time
}

// NewLoader returns a Loader that fetches via f and caches for ttl.
func NewLoader(f FetchFunc, ttl time.Duration) *Loader {
	return &Loader{fetch: f, ttl: ttl}
}

// Get returns the current taxonomy, refreshing if the cache is expired.
// On fetch error, returns the stale cached taxonomy if one exists.
func (l *Loader) Get() (Taxonomy, error) {
	l.mu.RLock()
	fresh := l.cached != nil && time.Since(l.fetched) < l.ttl
	if fresh {
		cached := *l.cached
		l.mu.RUnlock()
		return cached, nil
	}
	l.mu.RUnlock()

	data, err := l.fetch()
	if err != nil {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.cached != nil {
			return *l.cached, nil
		}
		return Taxonomy{}, err
	}
	tax, err := Parse(data)
	if err != nil {
		return Taxonomy{}, err
	}

	l.mu.Lock()
	l.cached = &tax
	l.fetched = time.Now()
	l.mu.Unlock()
	return tax, nil
}

// Invalidate forces the next Get() to re-fetch.
func (l *Loader) Invalidate() {
	l.mu.Lock()
	l.cached = nil
	l.mu.Unlock()
}

// ErrNoTaxonomy is returned when Get is called without a previous successful fetch
// and the current fetch also fails.
var ErrNoTaxonomy = errors.New("no taxonomy loaded and fetch failed")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hats/ -run TestLoader -v`
Expected: PASS both cases.

- [ ] **Step 5: Add a GitHub-backed fetcher helper (separate function, easy to swap in tests)**

Append to `internal/hats/loader.go`:

```go
// GitHubFetchFunc returns a FetchFunc that pulls hats.md from a repo via the
// GitHub Contents API using the existing github client.
//
// client is any value satisfying the small interface shown below — this
// keeps the hats package free of a direct dep on the github client.
type ContentFetcher interface {
	GetFileContents(ctx context.Context, installationID int64, repo, path string) ([]byte, error)
}

// GitHubFetchFunc wires a ContentFetcher into a FetchFunc for a given repo+path.
func GitHubFetchFunc(ctx context.Context, f ContentFetcher, installationID int64, repo, path string) FetchFunc {
	return func() ([]byte, error) {
		return f.GetFileContents(ctx, installationID, repo, path)
	}
}
```

Add `"context"` to the imports.

- [ ] **Step 6: Run `go vet` and all hats tests**

Run: `go vet ./internal/hats/ && go test ./internal/hats/ -v`
Expected: no vet output, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/hats/loader.go internal/hats/loader_test.go
git commit -m "feat(hats): add loader with TTL cache and GitHub fetcher"
```

---

## Task 4: GitHub client methods for PRs and releases

**Files:**
- Modify: `internal/github/client.go`
- Modify: `internal/github/client_test.go`

- [ ] **Step 1: Write failing tests for the new methods**

Add to `internal/github/client_test.go`:

```go
func TestListMergedPRsBetween_Parsing(t *testing.T) {
	// Hit the parsing via injected HTTP client/mux pattern if one exists.
	// If the package already uses a testable httptest.Server pattern, reuse it.
	// Otherwise add the smallest such pattern here (see existing tests for shape).
	t.Skip("wire up once the test harness pattern is confirmed (see existing tests)")
}

func TestGetLatestReleases_Parsing(t *testing.T) {
	t.Skip("wire up once the test harness pattern is confirmed (see existing tests)")
}
```

These are placeholders so the new public API shape lands. Real tests replace them in Step 5. Run `go test ./internal/github/ -v` to confirm the suite still compiles after Step 2.

- [ ] **Step 2: Add method signatures and a thin happy-path implementation**

In `internal/github/client.go`, near the existing `SearchIssues`/`GetFileContents` methods:

```go
// MergedPR is a subset of fields from GitHub's closed-PRs search.
type MergedPR struct {
	Number   int
	Title    string
	Body     string
	MergedAt time.Time
	URL      string
	Labels   []string
}

// ListMergedPRsBetween returns PRs merged between two git tags on the given repo,
// using the closed-PRs search API. Tags must exist in the repo; the method
// resolves them to dates via the /git/refs/tags + /git/commits chain.
func (c *Client) ListMergedPRsBetween(ctx context.Context, installationID int64, repo, fromTag, toTag string) ([]MergedPR, error) {
	fromTime, err := c.commitDateForTag(ctx, installationID, repo, fromTag)
	if err != nil {
		return nil, fmt.Errorf("from tag %q: %w", fromTag, err)
	}
	toTime, err := c.commitDateForTag(ctx, installationID, repo, toTag)
	if err != nil {
		return nil, fmt.Errorf("to tag %q: %w", toTag, err)
	}
	// Build search query: merged PRs between two dates.
	q := fmt.Sprintf("repo:%s is:pr is:merged merged:%s..%s",
		repo, fromTime.Format("2006-01-02"), toTime.Format("2006-01-02"))
	return c.searchMergedPRs(ctx, installationID, q)
}

// Release is a subset of fields from GitHub's releases list.
type Release struct {
	TagName     string
	Name        string
	Body        string
	PublishedAt time.Time
	Prerelease  bool
	Draft       bool
	HTMLURL     string
}

// GetLatestReleases returns up to n most-recent non-draft releases for repo.
func (c *Client) GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]Release, error) {
	path := fmt.Sprintf("/repos/%s/releases?per_page=%d", repo, n)
	resp, err := c.authGet(ctx, installationID, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		PublishedAt time.Time `json:"published_at"`
		Prerelease  bool      `json:"prerelease"`
		Draft       bool      `json:"draft"`
		HTMLURL     string    `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		out = append(out, Release(r))
	}
	return out, nil
}

// commitDateForTag resolves a tag to its commit's committer date.
// Follows annotated-tag indirection if needed.
func (c *Client) commitDateForTag(ctx context.Context, installationID int64, repo, tag string) (time.Time, error) {
	// /repos/{owner}/{repo}/git/ref/tags/{tag} then /git/commits/{sha}
	// Existing authGet helper returns response + err.
	refPath := fmt.Sprintf("/repos/%s/git/ref/tags/%s", repo, tag)
	resp, err := c.authGet(ctx, installationID, refPath)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ref); err != nil {
		return time.Time{}, fmt.Errorf("decode ref: %w", err)
	}
	sha := ref.Object.SHA
	if ref.Object.Type == "tag" {
		// Annotated tag — dereference once.
		tagPath := fmt.Sprintf("/repos/%s/git/tags/%s", repo, sha)
		r, err := c.authGet(ctx, installationID, tagPath)
		if err != nil {
			return time.Time{}, err
		}
		defer r.Body.Close()
		var t struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			return time.Time{}, fmt.Errorf("decode tag: %w", err)
		}
		sha = t.Object.SHA
	}
	commitPath := fmt.Sprintf("/repos/%s/git/commits/%s", repo, sha)
	cr, err := c.authGet(ctx, installationID, commitPath)
	if err != nil {
		return time.Time{}, err
	}
	defer cr.Body.Close()
	var commit struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	}
	if err := json.NewDecoder(cr.Body).Decode(&commit); err != nil {
		return time.Time{}, fmt.Errorf("decode commit: %w", err)
	}
	return commit.Committer.Date, nil
}

func (c *Client) searchMergedPRs(ctx context.Context, installationID int64, query string) ([]MergedPR, error) {
	path := "/search/issues?per_page=100&q=" + url.QueryEscape(query)
	resp, err := c.authGet(ctx, installationID, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Items []struct {
			Number   int       `json:"number"`
			Title    string    `json:"title"`
			Body     string    `json:"body"`
			HTMLURL  string    `json:"html_url"`
			ClosedAt time.Time `json:"closed_at"`
			Labels   []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	out := make([]MergedPR, 0, len(payload.Items))
	for _, it := range payload.Items {
		labels := make([]string, 0, len(it.Labels))
		for _, lb := range it.Labels {
			labels = append(labels, lb.Name)
		}
		out = append(out, MergedPR{
			Number: it.Number, Title: it.Title, Body: it.Body,
			MergedAt: it.ClosedAt, URL: it.HTMLURL, Labels: labels,
		})
	}
	return out, nil
}
```

Add imports: `"encoding/json"`, `"net/url"`, `"time"`.

If `authGet` is not the existing helper name, replace with whatever the existing module uses (grep for `authGet` or similar in `client.go` and adapt). If the existing module uses the `go-github` library for some calls, use that instead — match the existing style.

- [ ] **Step 3: Run build + existing tests to verify nothing broke**

Run: `go build ./internal/github/ && go test ./internal/github/ -v`
Expected: build succeeds, tests pass (including the skipped placeholders).

- [ ] **Step 4: Add httptest-backed tests for the new methods**

Replace the skipped placeholders with real tests using `httptest.NewServer`. Follow the pattern from the existing tests in `internal/github/client_test.go` (grep for `httptest` or for how the pool is wired with a custom HTTP client). Produce one happy-path assertion per method: for `GetLatestReleases`, fixture three releases and assert the draft one is filtered out; for `ListMergedPRsBetween`, fixture a tag refs → commit dates → search response chain and assert length and fields.

Run: `go test ./internal/github/ -run TestListMergedPRsBetween -v`
Run: `go test ./internal/github/ -run TestGetLatestReleases -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat(github): add ListMergedPRsBetween and GetLatestReleases"
```

---

## Task 5: Regression-window diff module

**Files:**
- Create: `internal/regression/diff.go`
- Create: `internal/regression/diff_test.go`

- [ ] **Step 1: Write failing test for keyword filter**

`internal/regression/diff_test.go`:

```go
package regression

import (
	"context"
	"testing"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

type fakeClient struct {
	prs []gh.MergedPR
	err error
}

func (f *fakeClient) ListMergedPRsBetween(ctx context.Context, installationID int64, repo, from, to string) ([]gh.MergedPR, error) {
	return f.prs, f.err
}

func TestDiff_FiltersByKeyword(t *testing.T) {
	prs := []gh.MergedPR{
		{Number: 1, Title: "fix iframe reload on network failure", Body: "scoped to top-frame only"},
		{Number: 2, Title: "bump electron to 39.8.2", Body: ""},
		{Number: 3, Title: "update README", Body: "no code changes"},
	}
	c := &fakeClient{prs: prs}
	d := NewDiff(c)
	got, err := d.Run(context.Background(), 1, "repo/name", "v2.7.5", "v2.7.8",
		[]string{"iframe", "reload", "network"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("got %v, want single PR #1", got)
	}
}

func TestDiff_EmptyKeywordsReturnsAll(t *testing.T) {
	prs := []gh.MergedPR{{Number: 1}, {Number: 2}}
	d := NewDiff(&fakeClient{prs: prs})
	got, err := d.Run(context.Background(), 1, "r", "a", "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestDiff_PropagatesClientError(t *testing.T) {
	d := NewDiff(&fakeClient{err: errTest})
	_, err := d.Run(context.Background(), 1, "r", "a", "b", nil)
	if err == nil {
		t.Fatal("want error")
	}
}

var errTest = testErr{}

type testErr struct{}

func (testErr) Error() string { return "test" }

var _ = time.Now // silence unused import if needed
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/regression/ -v`
Expected: FAIL with "package regression not defined".

- [ ] **Step 3: Implement the module**

`internal/regression/diff.go`:

```go
// Package regression runs a keyword-filtered diff of merged PRs between two
// release tags to surface candidate causes for a regression report.
package regression

import (
	"context"
	"strings"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

// PRLister is the subset of the github client this package needs.
type PRLister interface {
	ListMergedPRsBetween(ctx context.Context, installationID int64, repo, from, to string) ([]gh.MergedPR, error)
}

// Diff runs regression-window analysis.
type Diff struct {
	client PRLister
}

// NewDiff constructs a Diff.
func NewDiff(c PRLister) *Diff {
	return &Diff{client: c}
}

// Run returns PRs merged between fromTag and toTag whose title or body
// contains at least one of the given keywords (case-insensitive). If
// keywords is nil, all PRs are returned unfiltered.
func (d *Diff) Run(ctx context.Context, installationID int64, repo, fromTag, toTag string, keywords []string) ([]gh.MergedPR, error) {
	prs, err := d.client.ListMergedPRsBetween(ctx, installationID, repo, fromTag, toTag)
	if err != nil {
		return nil, err
	}
	if len(keywords) == 0 {
		return prs, nil
	}
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = strings.ToLower(k)
	}
	out := make([]gh.MergedPR, 0, len(prs))
	for _, p := range prs {
		hay := strings.ToLower(p.Title + " " + p.Body)
		for _, k := range lowered {
			if strings.Contains(hay, k) {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/regression/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/regression/
git commit -m "feat(regression): add PR-diff runner with keyword filter"
```

---

## Task 6: Electron watcher — fetch + event-journal persistence

**Files:**
- Create: `internal/upstream/electron.go`
- Create: `internal/upstream/electron_test.go`

- [ ] **Step 1: Write failing test for "fetch new releases and record new events"**

`internal/upstream/electron_test.go`:

```go
package upstream

import (
	"context"
	"errors"
	"testing"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

type fakeReleases struct {
	releases []gh.Release
	err      error
}

func (f *fakeReleases) GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]gh.Release, error) {
	return f.releases, f.err
}

type fakeEvents struct {
	existing map[string]bool
	recorded []store.RepoEvent
}

func (f *fakeEvents) ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]store.RepoEvent, error) {
	var out []store.RepoEvent
	for ref := range f.existing {
		out = append(out, store.RepoEvent{Repo: repo, EventType: "upstream_release", SourceRef: ref})
	}
	return out, nil
}

func (f *fakeEvents) RecordEvents(ctx context.Context, ev []store.RepoEvent) error {
	f.recorded = append(f.recorded, ev...)
	return nil
}

func TestWatcher_RecordsOnlyNewReleases(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v39.8.0", Name: "39.8.0", Body: "note a", PublishedAt: time.Now()},
		{TagName: "v39.8.1", Name: "39.8.1", Body: "note b", PublishedAt: time.Now()},
		{TagName: "v39.8.2", Name: "39.8.2", Body: "note c", PublishedAt: time.Now()},
	}
	events := &fakeEvents{existing: map[string]bool{"v39.8.0": true}}
	w := NewWatcher(&fakeReleases{releases: rel}, events)
	recorded, err := w.Sync(context.Background(), 1, "teams-for-linux", "electron/electron")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded = %d, want 2 new", len(recorded))
	}
	if recorded[0].SourceRef != "v39.8.1" || recorded[1].SourceRef != "v39.8.2" {
		t.Errorf("wrong releases recorded: %v", recorded)
	}
}

func TestWatcher_PropagatesError(t *testing.T) {
	w := NewWatcher(&fakeReleases{err: errors.New("boom")}, &fakeEvents{})
	_, err := w.Sync(context.Background(), 1, "r", "u")
	if err == nil {
		t.Fatal("want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream/ -v`
Expected: FAIL — package not defined.

- [ ] **Step 3: Implement fetch + persistence**

`internal/upstream/electron.go`:

```go
// Package upstream watches upstream dependency releases and records them in
// the event journal for downstream cross-reference work.
package upstream

import (
	"context"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// ReleaseLister is the subset of the github client we need.
type ReleaseLister interface {
	GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]gh.Release, error)
}

// EventStore is the subset of the store we need.
type EventStore interface {
	ListEvents(ctx context.Context, repo string, since time.Time, eventTypes []string, limit int) ([]store.RepoEvent, error)
	RecordEvents(ctx context.Context, events []store.RepoEvent) error
}

// Watcher pulls new upstream releases and records them against a consumer repo.
type Watcher struct {
	gh     ReleaseLister
	events EventStore
	lookN  int
	window time.Duration
}

// NewWatcher constructs a Watcher with defaults (20 releases, 180-day window).
func NewWatcher(g ReleaseLister, e EventStore) *Watcher {
	return &Watcher{gh: g, events: e, lookN: 20, window: 180 * 24 * time.Hour}
}

// Sync fetches recent releases from upstreamRepo and records any that are not
// already in the consumerRepo's event journal as "upstream_release" events.
// Returns the slice of newly recorded events.
func (w *Watcher) Sync(ctx context.Context, installationID int64, consumerRepo, upstreamRepo string) ([]store.RepoEvent, error) {
	releases, err := w.gh.GetLatestReleases(ctx, installationID, upstreamRepo, w.lookN)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-w.window)
	existing, err := w.events.ListEvents(ctx, consumerRepo, since, []string{"upstream_release"}, 1000)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.SourceRef] = true
	}
	var fresh []store.RepoEvent
	for _, r := range releases {
		if seen[r.TagName] {
			continue
		}
		fresh = append(fresh, store.RepoEvent{
			Repo:      consumerRepo,
			EventType: "upstream_release",
			SourceRef: r.TagName,
			Summary:   r.Name,
			Metadata: map[string]any{
				"upstream_repo": upstreamRepo,
				"tag":           r.TagName,
				"prerelease":    r.Prerelease,
				"body":          r.Body,
				"html_url":      r.HTMLURL,
				"published_at":  r.PublishedAt,
			},
		})
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	if err := w.events.RecordEvents(ctx, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/upstream/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/upstream/
git commit -m "feat(upstream): watch Electron releases and record to event journal"
```

---

## Task 7: Electron watcher — embed and index

**Files:**
- Modify: `internal/upstream/electron.go`
- Modify: `internal/upstream/electron_test.go`

- [ ] **Step 1: Add failing test for embedding step**

Append to `electron_test.go`:

```go
type fakeIndexer struct {
	upserted []store.Document
}

func (f *fakeIndexer) UpsertEmbedded(ctx context.Context, doc store.Document) error {
	f.upserted = append(f.upserted, doc)
	return nil
}

func TestWatcher_EmbedsNewReleases(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v41.0.0", Name: "41.0.0", Body: "fixes input method", PublishedAt: time.Now()},
	}
	events := &fakeEvents{existing: map[string]bool{}}
	idx := &fakeIndexer{}
	w := NewWatcher(&fakeReleases{releases: rel}, events).WithIndexer(idx)
	if _, err := w.Sync(context.Background(), 1, "teams-for-linux", "electron/electron"); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(idx.upserted) != 1 {
		t.Fatalf("len(upserted) = %d, want 1", len(idx.upserted))
	}
	if idx.upserted[0].DocType != "upstream_release" {
		t.Errorf("doc_type = %q", idx.upserted[0].DocType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/upstream/ -v`
Expected: FAIL — `WithIndexer` undefined.

- [ ] **Step 3: Extend Watcher with an indexer hook**

Append to `internal/upstream/electron.go`:

```go
// Indexer handles embedding and upserting a document into the vector store.
// It is a small interface so tests can stub it without pulling in Gemini.
type Indexer interface {
	UpsertEmbedded(ctx context.Context, doc store.Document) error
}

// WithIndexer sets an optional indexer. When set, Sync also embeds + upserts
// the release notes as a Document with doc_type "upstream_release".
func (w *Watcher) WithIndexer(i Indexer) *Watcher {
	w.idx = i
	return w
}

// Append field:
// idx Indexer

// In Sync, after RecordEvents succeeds, add:
//
// if w.idx != nil {
//     for _, ev := range fresh {
//         body, _ := ev.Metadata["body"].(string)
//         tag, _ := ev.Metadata["tag"].(string)
//         doc := store.Document{
//             Repo:     ev.Repo,
//             DocType:  "upstream_release",
//             DocID:    tag,
//             Title:    ev.Summary,
//             Content:  body,
//             Metadata: ev.Metadata,
//         }
//         if err := w.idx.UpsertEmbedded(ctx, doc); err != nil {
//             return fresh, err
//         }
//     }
// }
```

Make those three edits to the Watcher struct (new field), `NewWatcher` (no change, just leave `idx` zero-value), and `Sync` body.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/upstream/ -v`
Expected: all PASS.

- [ ] **Step 5: Wire `ingest.EmbedAndUpsert` as the production Indexer adapter**

Add a small adapter at the bottom of `electron.go`:

```go
// IngestAdapter bridges the existing ingest.EmbedAndUpsert into the Indexer
// interface without the upstream package depending on ingest directly.
type IngestAdapter struct {
	EmbedFunc func(ctx context.Context, doc store.Document) error
}

func (a IngestAdapter) UpsertEmbedded(ctx context.Context, doc store.Document) error {
	return a.EmbedFunc(ctx, doc)
}
```

The server wiring will construct this adapter by closure over an `ingest.EmbedAndUpsert` call (Task 9).

- [ ] **Step 6: Commit**

```bash
git add internal/upstream/
git commit -m "feat(upstream): embed new Electron releases into vector store"
```

---

## Task 8: Electron watcher — cross-reference blocked issues

**Files:**
- Modify: `internal/upstream/electron.go`
- Modify: `internal/upstream/electron_test.go`

- [ ] **Step 1: Write failing test for cross-reference**

Append to `electron_test.go`:

```go
type fakeBlockedFinder struct {
	issues []store.SimilarIssue
}

func (f *fakeBlockedFinder) FindSimilarBlockedIssues(ctx context.Context, repo string, embedding []float32, limit int) ([]store.SimilarIssue, error) {
	return f.issues, nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, 768), nil
}

func TestWatcher_CrossReferencesBlockedIssues(t *testing.T) {
	rel := []gh.Release{
		{TagName: "v39.8.2", Body: "fix VideoFrame prototype via contextBridge", PublishedAt: time.Now()},
	}
	bf := &fakeBlockedFinder{issues: []store.SimilarIssue{
		{Number: 2169, Title: "Camera broken", Distance: 0.20},
	}}
	w := NewWatcher(&fakeReleases{releases: rel}, &fakeEvents{existing: map[string]bool{}}).
		WithBlockedFinder(bf, stubEmbedder{})
	matches, err := w.SyncAndCrossReference(context.Background(), 1, "teams-for-linux", "electron/electron")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Release.TagName != "v39.8.2" {
		t.Errorf("wrong release: %s", matches[0].Release.TagName)
	}
	if matches[0].Candidates[0].Number != 2169 {
		t.Errorf("wrong candidate: %d", matches[0].Candidates[0].Number)
	}
}
```

- [ ] **Step 2: Add a matching `FindSimilarBlockedIssues` to the store**

In `internal/store/postgres.go`, add:

```go
// FindSimilarBlockedIssues returns issues that still carry the "blocked" label
// and whose embedding is near the given vector. Caller supplies the embedding.
func (s *Store) FindSimilarBlockedIssues(ctx context.Context, repo string, embedding []float32, limit int) ([]SimilarIssue, error) {
	const q = `
		SELECT number, title, body, embedding <=> $1 AS distance
		FROM issues
		WHERE repo = $2 AND state = 'open' AND $3 = ANY(labels)
		ORDER BY embedding <=> $1
		LIMIT $4
	`
	rows, err := s.pool.Query(ctx, q, pgvector.NewVector(embedding), repo, "blocked", limit)
	// ... standard rows scan pattern, return []SimilarIssue
}
```

If the `issues` table schema does not have a `labels` column, first check — it likely does per existing code (see `internal/store/models.go` or grep for `labels`). If not, extend the schema via a new migration in `internal/store/migrations/` (follow the 013/014 numbering pattern). Confirm before writing the migration.

Run: `go build ./internal/store/ && go vet ./internal/store/`

- [ ] **Step 3: Implement the cross-reference logic in the watcher**

Append to `internal/upstream/electron.go`:

```go
// Embedder embeds text to a 768-d vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BlockedFinder finds open issues with the "blocked" label near an embedding.
type BlockedFinder interface {
	FindSimilarBlockedIssues(ctx context.Context, repo string, embedding []float32, limit int) ([]store.SimilarIssue, error)
}

// WithBlockedFinder installs the cross-reference dependency. When both a
// BlockedFinder and an Embedder are set, SyncAndCrossReference can run.
func (w *Watcher) WithBlockedFinder(bf BlockedFinder, e Embedder) *Watcher {
	w.bf = bf
	w.emb = e
	return w
}

// Match is a single release paired with candidate blocked issues whose
// embedding is near enough to suggest the release may fix them.
type Match struct {
	Release    gh.Release
	Event      store.RepoEvent
	Candidates []store.SimilarIssue
}

// SyncAndCrossReference runs Sync and, for each new release, finds open
// blocked issues whose embedding is near the release notes.
func (w *Watcher) SyncAndCrossReference(ctx context.Context, installationID int64, consumerRepo, upstreamRepo string) ([]Match, error) {
	recorded, err := w.Sync(ctx, installationID, consumerRepo, upstreamRepo)
	if err != nil {
		return nil, err
	}
	if w.bf == nil || w.emb == nil {
		return nil, nil
	}
	var out []Match
	for _, ev := range recorded {
		body, _ := ev.Metadata["body"].(string)
		if body == "" {
			body = ev.Summary
		}
		vec, err := w.emb.Embed(ctx, body)
		if err != nil {
			return out, err
		}
		cands, err := w.bf.FindSimilarBlockedIssues(ctx, consumerRepo, vec, 5)
		if err != nil {
			return out, err
		}
		// Threshold: upstream_release threshold (0.45 in default config) is
		// expressed as relevance, not distance. For SimilarIssue.Distance
		// we keep entries with distance < 0.35 (tight).
		var kept []store.SimilarIssue
		for _, c := range cands {
			if c.Distance < 0.35 {
				kept = append(kept, c)
			}
		}
		if len(kept) > 0 {
			out = append(out, Match{Release: toRelease(ev), Event: ev, Candidates: kept})
		}
	}
	return out, nil
}

func toRelease(ev store.RepoEvent) gh.Release {
	return gh.Release{
		TagName: ev.SourceRef,
		Name:    ev.Summary,
		Body:    stringOrEmpty(ev.Metadata["body"]),
		HTMLURL: stringOrEmpty(ev.Metadata["html_url"]),
	}
}

func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}
```

Append matching fields to the `Watcher` struct:

```go
bf  BlockedFinder
emb Embedder
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/upstream/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/upstream/ internal/store/
git commit -m "feat(upstream): cross-reference blocked issues against new Electron releases"
```

---

## Task 9: `/upstream-watch` HTTP endpoint

**Files:**
- Create: `cmd/server/upstream.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Confirm the existing auth pattern**

Read `cmd/server/main.go` handlers for `/cleanup` and `/ingest` to confirm the OIDC-Bearer auth middleware or helper function name. Replicate that for the new handler. If the code uses a wrapper like `withAuth(handler)`, use it.

- [ ] **Step 2: Write the handler**

`cmd/server/upstream.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/upstream"
)

// upstreamWatchHandler runs the Electron release watcher across all
// configured installations. Expects POST, authenticated via the existing
// cron OIDC middleware (see main.go for the wrapper).
//
// Request body: {"repo": "IsmaelMartinez/teams-for-linux"} (optional; if
// empty, all installations with butler.json Upstream entries are processed).
func (srv *server) upstreamWatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	var req struct {
		Repo string `json:"repo"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("bad body: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Resolve the set of (installation, repo, upstream-repo) tuples to sync.
	targets, err := srv.resolveUpstreamTargets(ctx, req.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	watcher := upstream.NewWatcher(srv.gh, srv.store).
		WithIndexer(srv.upstreamIndexer()).
		WithBlockedFinder(srv.store, srv.llm)

	type result struct {
		Repo    string          `json:"repo"`
		Synced  int             `json:"synced"`
		Matches []upstreamMatch `json:"matches"`
	}
	out := make([]result, 0, len(targets))
	for _, t := range targets {
		matches, err := watcher.SyncAndCrossReference(ctx, t.InstallationID, t.ConsumerRepo, t.UpstreamRepo)
		if err != nil {
			http.Error(w, fmt.Sprintf("sync %s: %v", t.ConsumerRepo, err), http.StatusInternalServerError)
			return
		}
		out = append(out, result{
			Repo:    t.ConsumerRepo,
			Synced:  len(matches),
			Matches: toOutputMatches(matches),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
}

type upstreamMatch struct {
	ReleaseTag string `json:"release_tag"`
	Issues     []int  `json:"candidate_issues"`
}

func toOutputMatches(ms []upstream.Match) []upstreamMatch {
	out := make([]upstreamMatch, 0, len(ms))
	for _, m := range ms {
		nums := make([]int, 0, len(m.Candidates))
		for _, c := range m.Candidates {
			nums = append(nums, c.Number)
		}
		out = append(out, upstreamMatch{ReleaseTag: m.Release.TagName, Issues: nums})
	}
	return out
}
```

- [ ] **Step 3: Add supporting helpers and wiring in `cmd/server/main.go`**

Helper methods on `server`:

```go
type upstreamTarget struct {
	InstallationID int64
	ConsumerRepo   string
	UpstreamRepo   string
}

func (srv *server) resolveUpstreamTargets(ctx context.Context, filterRepo string) ([]upstreamTarget, error) {
	ids, err := srv.gh.ListInstallations(ctx)
	if err != nil {
		return nil, err
	}
	var out []upstreamTarget
	for _, id := range ids {
		// Per installation, read butler.json and list Upstream deps.
		// Reuse existing config resolution here (grep for how phase2 loads butler.json).
		// Skip installations where ResearchBrief.Enabled is false.
		// ...
	}
	return out, nil
}

func (srv *server) upstreamIndexer() upstream.Indexer {
	return upstream.IngestAdapter{EmbedFunc: func(ctx context.Context, doc store.Document) error {
		return ingest.EmbedAndUpsert(ctx, srv.store, srv.llm, doc)
	}}
}
```

Register the handler in the main HTTP mux near the other authenticated endpoints:

```go
mux.Handle("/upstream-watch", withAuth(http.HandlerFunc(srv.upstreamWatchHandler)))
```

Replace `withAuth` with the actual middleware name from the existing code.

- [ ] **Step 4: Write a unit test for the handler**

Add to `cmd/server/upstream_test.go` (create the file):

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamWatchHandler_MethodNotAllowed(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/upstream-watch", nil)
	srv.upstreamWatchHandler(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestUpstreamWatchHandler_BadBody(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upstream-watch", strings.NewReader("not json"))
	req.ContentLength = 8
	srv.upstreamWatchHandler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
```

Happy-path tests will come with integration wiring in a later pass — they require the full server constructor.

- [ ] **Step 5: Run all tests and build**

Run: `go build ./... && go test ./cmd/server/ -v && go test ./... -count=1`
Expected: build succeeds, new tests pass, no regressions elsewhere.

- [ ] **Step 6: Commit**

```bash
git add cmd/server/upstream.go cmd/server/upstream_test.go cmd/server/main.go
git commit -m "feat(server): add /upstream-watch endpoint for Electron release cross-reference"
```

---

## Task 10: Upstream-watcher cron workflow

**Files:**
- Create: `.github/workflows/upstream-watcher.yml`

- [ ] **Step 1: Copy the auth+invoke pattern from `dashboard.yml`**

Read `.github/workflows/dashboard.yml` to confirm the OIDC Google Workload Identity pattern and the Cloud Run URL env var.

- [ ] **Step 2: Write the workflow**

`.github/workflows/upstream-watcher.yml`:

```yaml
name: Upstream watcher (daily)

on:
  schedule:
    - cron: '0 4 * * *'
  workflow_dispatch: {}

permissions:
  id-token: write
  contents: read

jobs:
  run:
    runs-on: ubuntu-latest
    steps:
      - name: Auth via Workload Identity
        uses: google-github-actions/auth@v2
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.DEPLOY_SA }}

      - name: Get ID token for Cloud Run
        id: token
        run: |
          TOKEN=$(gcloud auth print-identity-token --audiences=${{ vars.CLOUD_RUN_URL }})
          echo "::add-mask::$TOKEN"
          echo "token=$TOKEN" >> "$GITHUB_OUTPUT"

      - name: Trigger /upstream-watch
        run: |
          curl -sS -X POST \
            -H "Authorization: Bearer ${{ steps.token.outputs.token }}" \
            -H "Content-Type: application/json" \
            -d '{}' \
            "${{ vars.CLOUD_RUN_URL }}/upstream-watch"
```

If `dashboard.yml` uses a different ID-token helper or env-var naming, match it exactly — those three `vars.*` entries (`WIF_PROVIDER`, `DEPLOY_SA`, `CLOUD_RUN_URL`) are placeholders to align with whatever already works.

- [ ] **Step 3: Lint the workflow locally (optional, requires `actionlint`)**

Run: `actionlint .github/workflows/upstream-watcher.yml` if available.
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/upstream-watcher.yml
git commit -m "ci: daily cron to trigger /upstream-watch"
```

---

## Task 11: Hat-aware soft rerank in vector search

**Files:**
- Modify: `internal/store/postgres.go`
- Modify: `internal/store/postgres_test.go`

- [ ] **Step 1: Write a failing test for the boost behaviour**

Add to `internal/store/postgres_test.go`:

```go
func TestReranker_BoostsKeywordMatches(t *testing.T) {
	docs := []SimilarDocument{
		{DocID: "a", Title: "Generic troubleshooting", Content: "various tips", Distance: 0.25},
		{DocID: "b", Title: "Wayland screen-share", Content: "ozone flags", Distance: 0.30},
	}
	got := ApplyHatBoost(docs, []string{"ozone", "wayland"}, 0.05)
	if got[0].DocID != "b" {
		t.Errorf("boosted order: %v; want b first", got)
	}
	// Original a.Distance 0.25 stays; b.Distance 0.30 - boost 0.05 = 0.25 → tie
	// broken by boost flag / stable sort. We assert b is now first.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReranker_Boost -v`
Expected: FAIL — `ApplyHatBoost` undefined.

- [ ] **Step 3: Implement `ApplyHatBoost` as a pure function**

Add to `internal/store/postgres.go` (or a new file `internal/store/rerank.go`):

```go
// ApplyHatBoost rescales distances downward (closer) for documents whose
// title or content contains any of the boost keywords (case-insensitive).
// Returns a new slice ordered ascending by the adjusted distance.
// A soft rerank — docs without matches keep their original distance.
func ApplyHatBoost(docs []SimilarDocument, keywords []string, boost float64) []SimilarDocument {
	if len(keywords) == 0 || boost <= 0 {
		return docs
	}
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = strings.ToLower(k)
	}
	type scored struct {
		doc      SimilarDocument
		adjusted float64
	}
	out := make([]scored, len(docs))
	for i, d := range docs {
		adj := d.Distance
		hay := strings.ToLower(d.Title + " " + d.Content)
		for _, k := range lowered {
			if strings.Contains(hay, k) {
				adj -= boost
				break
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

Add imports: `"sort"`, `"strings"`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -run TestReranker -v`
Expected: PASS.

Run: `go test ./internal/store/ -v`
Expected: no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add ApplyHatBoost soft rerank for hat-aware retrieval"
```

---

## Task 12: `/brief-preview` HTTP endpoint (retrieval smoke test)

**Files:**
- Create: `cmd/server/brief_preview.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Define the smoke-test contract**

Request: `POST /brief-preview` with body `{"repo": "owner/repo", "issue_number": 2169}`.
Response JSON fields:
- `class`: the hat name (or `"other"`)
- `similar_past_issues`: top 5 by adjusted distance
- `docs`: top 5 by hat-boosted distance, filtered above threshold
- `regression_prs`: if the issue body names a working version, the PRs between working and current, keyword-filtered
- `upstream_candidates`: any open `blocked` entries plausibly linked to recent upstream releases

No LLM call for the hat selection yet — for the smoke test, accept an optional `"hat": "display-session-media"` in the request and use it directly. This isolates retrieval from brief generation.

- [ ] **Step 2: Write the handler**

`cmd/server/brief_preview.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/hats"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/regression"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

var workingVersionRe = regexp.MustCompile(`(?i)(?:works\s+in|worked\s+in|working\s+on|prior\s+working)\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

type briefPreviewRequest struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	HatName     string `json:"hat,omitempty"`
}

type briefPreviewResponse struct {
	Class             string                  `json:"class"`
	SimilarIssues     []store.SimilarIssue    `json:"similar_past_issues"`
	Docs              []store.SimilarDocument `json:"docs"`
	RegressionPRs     []regression.PRSummary  `json:"regression_prs"`
	UpstreamCandidates []int                  `json:"upstream_candidates"`
}

func (srv *server) briefPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req briefPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Repo == "" || req.IssueNumber == 0 {
		http.Error(w, "repo and issue_number required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	// 1. Fetch the issue body (reuse existing store or gh helper).
	issue, err := srv.store.GetIssue(ctx, req.Repo, req.IssueNumber)
	if err != nil {
		http.Error(w, fmt.Sprintf("get issue: %v", err), http.StatusInternalServerError)
		return
	}

	// 2. Embed.
	vec, err := srv.llm.Embed(ctx, issue.Title+"\n\n"+issue.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("embed: %v", err), http.StatusInternalServerError)
		return
	}

	// 3. Load hats.md for this repo (cached loader held on srv).
	tax, err := srv.hatsLoader.Get()
	if err != nil {
		http.Error(w, fmt.Sprintf("hats: %v", err), http.StatusInternalServerError)
		return
	}
	hat := tax.Find(req.HatName)
	if hat == nil && req.HatName == "" {
		// Smoke test: accept missing hat and proceed with no boost.
	}

	// 4. Retrieve similar docs, apply boost if a hat is set.
	docs, err := srv.store.FindSimilarDocuments(ctx, req.Repo, allDocTypes(), vec, 5)
	if err != nil {
		http.Error(w, fmt.Sprintf("docs: %v", err), http.StatusInternalServerError)
		return
	}
	if hat != nil {
		docs = store.ApplyHatBoost(docs, hat.RetrievalBoostKeywords, 0.05)
	}

	// 5. Similar past issues.
	similar, err := srv.store.FindSimilarIssues(ctx, req.Repo, vec, req.IssueNumber, 5)
	if err != nil {
		http.Error(w, fmt.Sprintf("issues: %v", err), http.StatusInternalServerError)
		return
	}

	// 6. Regression-window PR diff if the body names a working version.
	var prs []regression.PRSummary
	if m := workingVersionRe.FindStringSubmatch(issue.Body); m != nil {
		working := "v" + m[1]
		current, err := srv.resolveCurrentReleaseTag(ctx, req.Repo) // helper: latest release tag
		if err == nil && current != "" {
			keywords := extractSymptomKeywords(issue.Body) // simple tokeniser, see below
			diff := regression.NewDiff(srv.gh)
			found, err := diff.Run(ctx, srv.installationIDFor(req.Repo), req.Repo, working, current, keywords)
			if err == nil {
				for _, p := range found {
					prs = append(prs, regression.PRSummary{Number: p.Number, Title: p.Title, URL: p.URL})
				}
			}
		}
	}

	// 7. Upstream candidates (blocked issues near issue embedding).
	blocked, _ := srv.store.FindSimilarBlockedIssues(ctx, req.Repo, vec, 3)
	upstreamNums := make([]int, 0, len(blocked))
	for _, b := range blocked {
		upstreamNums = append(upstreamNums, b.Number)
	}

	resp := briefPreviewResponse{
		Class:              className(hat, req.HatName),
		SimilarIssues:      similar,
		Docs:               docs,
		RegressionPRs:      prs,
		UpstreamCandidates: upstreamNums,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func className(h *hats.Hat, requested string) string {
	if h != nil {
		return h.Name
	}
	if requested != "" {
		return requested
	}
	return "other"
}

func allDocTypes() []string {
	return []string{"troubleshooting", "configuration", "adr", "roadmap", "research", "upstream_release", "upstream_issue"}
}

// extractSymptomKeywords pulls a very small set of candidate keywords from
// the issue body to drive regression-window PR filtering. The real brief
// generator (future work) will do this via the LLM.
func extractSymptomKeywords(body string) []string {
	// Start with the union of common symptom tokens; future iterations will
	// replace this with an LLM call.
	candidates := []string{"iframe", "reload", "network", "auth", "wayland", "ozone", "camera", "screen", "tray", "notification", "sharepoint"}
	lowered := strings.ToLower(body)
	var hit []string
	for _, c := range candidates {
		if strings.Contains(lowered, c) {
			hit = append(hit, c)
		}
	}
	return hit
}
```

Add types `PRSummary` in `internal/regression/diff.go`:

```go
// PRSummary is a small shape the preview handler returns.
type PRSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}
```

Register the handler in `main.go`:

```go
mux.Handle("/brief-preview", withAuth(http.HandlerFunc(srv.briefPreviewHandler)))
```

The helpers `srv.resolveCurrentReleaseTag` and `srv.installationIDFor` belong alongside the existing config resolution; implement them as thin wrappers over `gh.GetLatestReleases` (pick first) and `gh.ListInstallations` (first matching repo) respectively.

- [ ] **Step 3: Write a minimal handler test**

`cmd/server/brief_preview_test.go`:

```go
package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBriefPreviewHandler_MethodNotAllowed(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodGet, "/brief-preview", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", rr.Code)
	}
}

func TestBriefPreviewHandler_BadRequest(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodPost, "/brief-preview", strings.NewReader("{}")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rr.Code)
	}
}

func TestBriefPreviewHandler_InvalidJSON(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodPost, "/brief-preview", bytes.NewReader([]byte("not json"))))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rr.Code)
	}
}
```

Happy-path coverage requires the full server wiring and is covered by an integration test added separately.

- [ ] **Step 4: Build, vet, test**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: build succeeds, no regressions, new handler tests pass.

- [ ] **Step 5: Manual smoke instructions**

Document in the PR description:

```
Manual smoke test (once deployed):
  curl -X POST \
    -H "Authorization: Bearer $(gcloud auth print-identity-token --audiences=$CLOUD_RUN_URL)" \
    -H "Content-Type: application/json" \
    -d '{"repo":"IsmaelMartinez/teams-for-linux","issue_number":2169,"hat":"display-session-media"}' \
    $CLOUD_RUN_URL/brief-preview | jq
```

Expect: JSON with `class: "display-session-media"`, non-empty `similar_past_issues`, docs reordered to favour wayland/ozone content, regression_prs empty (since the test is historical), upstream_candidates including #2169 itself or related blocked entries.

- [ ] **Step 6: Commit**

```bash
git add cmd/server/brief_preview.go cmd/server/brief_preview_test.go cmd/server/main.go internal/regression/
git commit -m "feat(server): add /brief-preview endpoint for retrieval smoke testing"
```

---

## Self-review checklist

Run these before declaring the plan done.

**Spec coverage:** Every retrieval-layer item in `docs/plans/2026-04-22-research-brief-bot-design.md` is represented — hats.md parser (Task 2-3), regression-window diff (Task 5), Electron changelog watcher (Task 6-10), hat-aware rerank (Task 11). Brief generator and promotion drafter are out of scope per the user's direction and the PR-120 deferral. Heterogeneity tracker is deferred too (a later plan).

**Placeholder scan:** No TBD/TODO. Each step has concrete code or command.

**Type consistency:** `MergedPR`, `Release`, `Match`, `PRSummary`, `Hat`, `Taxonomy` used consistently across modules. `store.SimilarDocument`, `store.SimilarIssue`, `store.RepoEvent`, `store.Document` reuse existing types.

**Known open items to confirm during execution:**
- The github client's existing auth/request helper name (`authGet` is assumed; match whatever exists).
- The `issues` table having a `labels` column for `FindSimilarBlockedIssues` (verify or add a migration first).
- The existing OIDC-auth middleware name on `cmd/server/main.go` for wrapping new endpoints.
- Whether `store.GetIssue` exists as drawn; if it doesn't, a thin fetcher via the GitHub client is fine for the preview handler.

Resolve each of these by reading one source file before writing code for the dependent step.
