package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
