// Package tools provides MCP tool definitions and HTTP helpers for the triage bot.
package tools

import (
	"bytes"
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
	return doRequest(http.MethodGet, url, secret, nil)
}

// postJSON makes a POST request with no body and returns the parsed JSON response.
func postJSON(url, secret string) (any, error) {
	return doRequest(http.MethodPost, url, secret, nil)
}

// postJSONWithBody makes a POST request with a JSON body and returns the parsed JSON response.
func postJSONWithBody(url, secret string, body []byte) (any, error) {
	return doRequest(http.MethodPost, url, secret, body)
}

func doRequest(method, url, secret string, body []byte) (any, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}
