package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewHealthStatusTool returns a Tool that calls the /health-check endpoint.
func NewHealthStatusTool(baseURL, secret string) Tool {
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
		Name:        "get_health_status",
		Description: "Returns operational health metrics: confidence score trends, stuck sessions, orphaned triage.",
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
		url := baseURL + "/health-check?repo=" + params.Repo
		return postJSON(url, secret)
	}

	return Tool{Def: def, Handler: handler}
}
