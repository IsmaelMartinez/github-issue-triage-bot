// Package tools provides MCP tool definitions and HTTP helpers for the triage bot.
package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// httpClient is the shared HTTP client with a reasonable timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Tool wraps a tool definition and its handler together.
type Tool struct {
	Def     mcp.ToolDef
	Handler mcp.ToolHandler
}

// fetchJSON makes a GET request and returns the parsed JSON response.
func fetchJSON(url, secret string) (any, error) {
	return doRequest(http.MethodGet, url, secret)
}

// postJSON makes a POST request and returns the parsed JSON response.
func postJSON(url, secret string) (any, error) {
	return doRequest(http.MethodPost, url, secret)
}

func doRequest(method, url, secret string) (any, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}
