package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewPendingTriageTool returns a Tool that calls the /report endpoint.
func NewPendingTriageTool(baseURL, secret string) Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"repo": {
				"type": "string",
				"description": "Repository in owner/repo format"
			}
		},
		"required": ["repo"]
	}`)

	def := mcp.ToolDef{
		Name:        "get_pending_triage",
		Description: "Returns pending triage and research shadow issues awaiting review.",
		InputSchema: schema,
	}

	handler := func(args json.RawMessage) (any, error) {
		var params struct {
			Repo string `json:"repo"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if params.Repo == "" {
			return nil, fmt.Errorf("repo is required")
		}
		url := baseURL + "/report?repo=" + params.Repo
		return fetchJSON(url, secret)
	}

	return Tool{Def: def, Handler: handler}
}
