package tools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewReportTrendsTool returns a Tool that calls the /report/trends endpoint.
func NewReportTrendsTool(baseURL, secret string) Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"repo": {
				"type": "string",
				"description": "Repository in owner/repo format"
			},
			"weeks": {
				"type": "integer",
				"description": "Number of weeks of trend data to return (default 12)"
			}
		},
		"required": ["repo"]
	}`)

	def := mcp.ToolDef{
		Name:        "get_report_trends",
		Description: "Returns weekly trend data and recent synthesis findings (clusters, drift, upstream impacts).",
		InputSchema: schema,
	}

	handler := func(args json.RawMessage) (any, error) {
		var params struct {
			Repo  string `json:"repo"`
			Weeks int    `json:"weeks"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if params.Repo == "" {
			return nil, fmt.Errorf("repo is required")
		}
		if params.Weeks <= 0 {
			params.Weeks = 12
		}
		url := baseURL + "/report/trends?repo=" + params.Repo + "&weeks=" + strconv.Itoa(params.Weeks)
		return fetchJSON(url, secret)
	}

	return Tool{Def: def, Handler: handler}
}
