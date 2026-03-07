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
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.github.com",
	}
}

// installationClient returns an HTTP client scoped to a specific installation.
func (c *Client) installationClient(installationID int64) (*http.Client, error) {
	itr, err := ghinstallation.New(http.DefaultTransport, c.appID, installationID, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("create installation transport: %w", err)
	}
	return &http.Client{Transport: itr, Timeout: 30 * time.Second}, nil
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
	Repo         RepoDetail       `json:"repository"`
	Installation InstallationInfo `json:"installation"`
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

// RepoDetail is the repository portion of a webhook event.
type RepoDetail struct {
	FullName string `json:"full_name"`
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
