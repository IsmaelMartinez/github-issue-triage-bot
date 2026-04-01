package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewSynthesisBriefingTool returns a Tool that fetches the last 4 weeks of synthesis findings.
func NewSynthesisBriefingTool(baseURL, secret string) Tool {
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
		Name:        "get_synthesis_briefing",
		Description: "Returns recent synthesis findings from the last 4 weekly briefings.",
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
		url := baseURL + "/report/trends?repo=" + params.Repo + "&weeks=4"
		return fetchJSON(url, secret)
	}

	return Tool{Def: def, Handler: handler}
}
