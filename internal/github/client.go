package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

// Client wraps the GitHub API for issue operations using GitHub App authentication.
type Client struct {
	appID      int64
	privateKey []byte
	httpClient *http.Client
	baseURL    string
}

// New creates a new GitHub API client authenticated as a GitHub App.
func New(appID int64, privateKey []byte) *Client {
	return &Client{
		appID:      appID,
		privateKey: privateKey,
		httpClient: &http.Client{Transport: newRetryTransport(http.DefaultTransport), Timeout: 30 * time.Second},
		baseURL:    "https://api.github.com",
	}
}

// WarmTLS issues one throwaway unauthenticated GET to api.github.com to prime
// DNS resolution and the TLS session before the first real request arrives.
// Cold-start Cloud Run instances have been observed to time out the TLS
// handshake on the first call to /app/installations/*/access_tokens, which
// silently drops maintainer `lgtm` signals.
func (c *Client) WarmTLS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/zen", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	// Drain fully so the connection returns to the idle pool for reuse.
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// installationClient returns an HTTP client scoped to a specific installation.
// The outer retry transport wraps the whole call (both the ghinstallation
// token-mint and the subsequent API request) so a transient TLS/5xx failure
// during either phase triggers a fresh attempt instead of silently dropping
// user signals like `lgtm`. One retry layer is intentional; nesting retries
// around ghinstallation's internal transport would amplify to N² attempts.
func (c *Client) installationClient(installationID int64) (*http.Client, error) {
	itr, err := ghinstallation.New(http.DefaultTransport, c.appID, installationID, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("create installation transport: %w", err)
	}
	if c.baseURL != "https://api.github.com" {
		itr.BaseURL = c.baseURL
	}
	return &http.Client{Transport: newRetryTransport(itr), Timeout: 30 * time.Second}, nil
}

// CreateComment posts a comment on a GitHub issue and returns the comment ID.
func (c *Client) CreateComment(ctx context.Context, installationID int64, repo string, issueNumber int, body string) (int64, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return 0, fmt.Errorf("installation client: %w", err)
	}

	payload := map[string]string{"body": body}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal comment: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.baseURL, repo, issueNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	return result.ID, nil
}

// ListComments returns all comments on a GitHub issue.
func (c *Client) ListComments(ctx context.Context, installationID int64, repo string, issueNumber int) ([]Comment, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.baseURL, repo, issueNumber)
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

	var comments []Comment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return comments, nil
}

// CreateIssue creates a new issue in a repository and returns the issue number.
func (c *Client) CreateIssue(ctx context.Context, installationID int64, repo, title, body string) (int, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return 0, fmt.Errorf("installation client: %w", err)
	}

	payload := map[string]string{"title": title, "body": body}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal issue: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues", c.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	return result.Number, nil
}

// CreateBranch creates a new branch from the main branch's HEAD.
func (c *Client) CreateBranch(ctx context.Context, installationID int64, repo, branchName string) error {
	client, err := c.installationClient(installationID)
	if err != nil {
		return fmt.Errorf("installation client: %w", err)
	}

	// Get SHA of main branch
	refURL := fmt.Sprintf("%s/repos/%s/git/ref/heads/main", c.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ref); err != nil {
		return fmt.Errorf("decode ref response: %w", err)
	}

	// Create branch
	payload := map[string]string{
		"ref": "refs/heads/" + branchName,
		"sha": ref.Object.SHA,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ref: %w", err)
	}

	createURL := fmt.Sprintf("%s/repos/%s/git/refs", c.baseURL, repo)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp2, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
		return fmt.Errorf("github API returned %d: %s", resp2.StatusCode, string(respBody))
	}

	return nil
}

// CreateOrUpdateFile creates or updates a file in a repository.
func (c *Client) CreateOrUpdateFile(ctx context.Context, installationID int64, repo, path, branch, message string, content []byte) error {
	client, err := c.installationClient(installationID)
	if err != nil {
		return fmt.Errorf("installation client: %w", err)
	}

	payload := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal file: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreatePullRequest creates a pull request and returns the PR number.
func (c *Client) CreatePullRequest(ctx context.Context, installationID int64, repo, title, body, head, base string) (int, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return 0, fmt.Errorf("installation client: %w", err)
	}

	payload := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal pull request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/pulls", c.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	return result.Number, nil
}

// ListInstallations returns all installation IDs for this GitHub App.
func (c *Client) ListInstallations(ctx context.Context) ([]int64, error) {
	appTransport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, c.appID, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("create app transport: %w", err)
	}
	client := &http.Client{Transport: appTransport, Timeout: 30 * time.Second}

	url := fmt.Sprintf("%s/app/installations", c.baseURL)
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

	var installations []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&installations); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	ids := make([]int64, len(installations))
	for i, inst := range installations {
		ids[i] = inst.ID
	}
	return ids, nil
}

// InstallationToken returns an access token for the given installation.
// This can be used for git clone/push operations via HTTPS.
func (c *Client) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	itr, err := ghinstallation.New(http.DefaultTransport, c.appID, installationID, c.privateKey)
	if err != nil {
		return "", fmt.Errorf("create installation transport: %w", err)
	}
	token, err := itr.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("get installation token: %w", err)
	}
	return token, nil
}

// FormatShadowIssueBody formats an issue body for a shadow repo mirror issue.
func FormatShadowIssueBody(sourceRepo string, issueNumber int, title, body string) string {
	return fmt.Sprintf("**Mirror of %s#%d**\n\n**Original title:** %s\n\n---\n\n%s", sourceRepo, issueNumber, title, body)
}

// Comment represents a GitHub issue comment.
type Comment struct {
	ID   int64       `json:"id"`
	Body string      `json:"body"`
	User CommentUser `json:"user"`
}

// CommentUser represents the user who made a comment.
type CommentUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// VerifyWebhookSignature verifies the GitHub webhook signature.
func VerifyWebhookSignature(payload []byte, signature string, secret string) bool {
	if len(signature) < 7 || signature[:7] != "sha256=" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature[7:]), []byte(expected))
}

// IssueCommentEvent represents a GitHub issue_comment webhook event payload.
type IssueCommentEvent struct {
	Action       string           `json:"action"`
	Issue        IssueDetail      `json:"issue"`
	Comment      CommentDetail    `json:"comment"`
	Repo         RepoDetail       `json:"repository"`
	Installation InstallationInfo `json:"installation"`
}

// CommentDetail is the comment portion of an issue_comment event.
type CommentDetail struct {
	ID   int64       `json:"id"`
	Body string      `json:"body"`
	User CommentUser `json:"user"`
}

// IssueEvent represents a GitHub issue webhook event payload.
type IssueEvent struct {
	Action       string           `json:"action"`
	Issue        IssueDetail      `json:"issue"`
	Changes      *IssueChanges    `json:"changes,omitempty"`
	Repo         RepoDetail       `json:"repository"`
	Installation InstallationInfo `json:"installation"`
}

// IssueChanges represents the changes payload sent by GitHub on issues.edited events.
type IssueChanges struct {
	Body *IssueChangeField `json:"body,omitempty"`
}

// IssueChangeField holds the previous value of a changed field.
type IssueChangeField struct {
	From string `json:"from"`
}

// InstallationInfo identifies the GitHub App installation that sent the event.
type InstallationInfo struct {
	ID int64 `json:"id"`
}

// IssueDetail is the issue portion of a webhook event.
type IssueDetail struct {
	Number int         `json:"number"`
	Title  string      `json:"title"`
	Body   string      `json:"body"`
	State  string      `json:"state"`
	Labels []LabelInfo `json:"labels"`
	User   IssueUser   `json:"user"`
}

// IssueUser is the user who created the issue.
type IssueUser struct {
	Login string `json:"login"`
}

// LabelInfo is a GitHub label.
type LabelInfo struct {
	Name string `json:"name"`
}

// PushEvent represents a GitHub push webhook event payload.
type PushEvent struct {
	Ref          string           `json:"ref"`
	Commits      []PushCommit     `json:"commits"`
	Repo         RepoDetail       `json:"repository"`
	Installation InstallationInfo `json:"installation"`
}

// PushCommit represents a single commit in a push event.
type PushCommit struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

// RepoDetail is the repository portion of a webhook event.
type RepoDetail struct {
	FullName string `json:"full_name"`
}

// IssueSearchResult represents a single issue returned from the GitHub search API.
type IssueSearchResult struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// SearchIssues searches for issues in a repository using the GitHub search API.
// The query is passed as the q parameter and URL-encoded internally.
// Callers should include repo: and is: qualifiers as raw strings.
func (c *Client) SearchIssues(ctx context.Context, installationID int64, query string) ([]IssueSearchResult, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/search/issues?q=%s", c.baseURL, neturl.QueryEscape(query))
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
		Items []IssueSearchResult `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result.Items, nil
}

// GetFileContents reads a file's contents from a repository via the Contents API.
// Returns the decoded content bytes and nil error, or nil bytes with nil error if the file does not exist (404).
func (c *Client) GetFileContents(ctx context.Context, installationID int64, repo, path string) ([]byte, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, repo, path)
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected encoding: %s", result.Encoding)
	}
	return base64.StdEncoding.DecodeString(result.Content)
}

// TreeEntry represents a single entry in a git tree (file or directory).
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

// GetTree fetches the full recursive file tree for a repository at the given ref (branch, tag, or SHA).
// Only blob entries (files) are returned; tree (directory) entries are filtered out.
// If the tree response is truncated, a warning is logged to stderr.
func (c *Client) GetTree(ctx context.Context, installationID int64, repo, ref string) ([]TreeEntry, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.baseURL, repo, ref)
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
		fmt.Fprintf(os.Stderr, "warning: git tree for %s@%s was truncated; some files may be missing\n", repo, ref)
	}

	var blobs []TreeEntry
	for _, entry := range result.Tree {
		if entry.Type == "blob" {
			blobs = append(blobs, entry)
		}
	}
	return blobs, nil
}

// MergedPR is a subset of fields from GitHub's closed-PRs search.
type MergedPR struct {
	Number   int
	Title    string
	Body     string
	MergedAt time.Time
	URL      string
	Labels   []string
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

// ListMergedPRsBetween returns PRs merged between two git tags on the given repo,
// using the closed-PRs search API. Tags must exist in the repo; the method
// resolves them to dates via the /git/ref/tags + /git/commits chain, following
// annotated-tag indirection when necessary.
func (c *Client) ListMergedPRsBetween(ctx context.Context, installationID int64, repo, fromTag, toTag string) ([]MergedPR, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	fromTime, err := c.commitDateForTag(ctx, client, repo, fromTag)
	if err != nil {
		return nil, fmt.Errorf("from tag %q: %w", fromTag, err)
	}
	toTime, err := c.commitDateForTag(ctx, client, repo, toTag)
	if err != nil {
		return nil, fmt.Errorf("to tag %q: %w", toTag, err)
	}

	query := fmt.Sprintf("repo:%s is:pr is:merged merged:%s..%s",
		repo, fromTime.Format("2006-01-02"), toTime.Format("2006-01-02"))
	return c.searchMergedPRs(ctx, client, query)
}

// GetLatestReleases returns up to n most-recent non-draft releases for repo.
func (c *Client) GetLatestReleases(ctx context.Context, installationID int64, repo string, n int) ([]Release, error) {
	if n <= 0 {
		n = 10
	}
	if n > 100 {
		n = 100
	}
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.baseURL, repo, n)
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
// Follows annotated-tag indirection (ref.Object.Type == "tag") by fetching
// /git/tags/{sha} before resolving the underlying commit.
func (c *Client) commitDateForTag(ctx context.Context, client *http.Client, repo, tag string) (time.Time, error) {
	refURL := fmt.Sprintf("%s/repos/%s/git/ref/tags/%s", c.baseURL, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refURL, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return time.Time{}, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

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
		tagURL := fmt.Sprintf("%s/repos/%s/git/tags/%s", c.baseURL, repo, sha)
		treq, err := http.NewRequestWithContext(ctx, http.MethodGet, tagURL, nil)
		if err != nil {
			return time.Time{}, fmt.Errorf("create request: %w", err)
		}
		treq.Header.Set("Accept", "application/vnd.github+json")

		tresp, err := client.Do(treq)
		if err != nil {
			return time.Time{}, fmt.Errorf("send request: %w", err)
		}
		defer tresp.Body.Close()

		if tresp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(io.LimitReader(tresp.Body, 4096))
			return time.Time{}, fmt.Errorf("github API returned %d: %s", tresp.StatusCode, string(respBody))
		}

		var annotated struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if err := json.NewDecoder(tresp.Body).Decode(&annotated); err != nil {
			return time.Time{}, fmt.Errorf("decode tag: %w", err)
		}
		sha = annotated.Object.SHA
	}

	commitURL := fmt.Sprintf("%s/repos/%s/git/commits/%s", c.baseURL, repo, sha)
	creq, err := http.NewRequestWithContext(ctx, http.MethodGet, commitURL, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("create request: %w", err)
	}
	creq.Header.Set("Accept", "application/vnd.github+json")

	cresp, err := client.Do(creq)
	if err != nil {
		return time.Time{}, fmt.Errorf("send request: %w", err)
	}
	defer cresp.Body.Close()

	if cresp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(cresp.Body, 4096))
		return time.Time{}, fmt.Errorf("github API returned %d: %s", cresp.StatusCode, string(respBody))
	}

	var commit struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	}
	if err := json.NewDecoder(cresp.Body).Decode(&commit); err != nil {
		return time.Time{}, fmt.Errorf("decode commit: %w", err)
	}
	return commit.Committer.Date, nil
}

func (c *Client) searchMergedPRs(ctx context.Context, client *http.Client, query string) ([]MergedPR, error) {
	url := fmt.Sprintf("%s/search/issues?per_page=100&q=%s", c.baseURL, neturl.QueryEscape(query))
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
			Number:   it.Number,
			Title:    it.Title,
			Body:     it.Body,
			MergedAt: it.ClosedAt,
			URL:      it.HTMLURL,
			Labels:   labels,
		})
	}
	return out, nil
}

// Issue is a minimal subset of a GitHub issue.
type Issue struct {
	Number int
	Title  string
	Body   string
	State  string
	URL    string
}

// GetIssue returns a single issue by number for the given repo.
func (c *Client) GetIssue(ctx context.Context, installationID int64, repo string, number int) (*Issue, error) {
	client, err := c.installationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("installation client: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d", c.baseURL, repo, number)
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

	var raw struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return &Issue{Number: raw.Number, Title: raw.Title, Body: raw.Body, State: raw.State, URL: raw.HTMLURL}, nil
}

// CloseIssue closes a GitHub issue by setting its state to "closed".
func (c *Client) CloseIssue(ctx context.Context, installationID int64, repo string, issueNumber int) error {
	client, err := c.installationClient(installationID)
	if err != nil {
		return fmt.Errorf("installation client: %w", err)
	}

	payload := map[string]string{"state": "closed"}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/issues/%d", c.baseURL, repo, issueNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
