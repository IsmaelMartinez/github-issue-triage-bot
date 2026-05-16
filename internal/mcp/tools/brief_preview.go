package tools

import (
	"encoding/json"
	"fmt"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mcp"
)

// NewBriefPreviewTool returns a Tool that calls the /brief-preview endpoint
// for hat-aware retrieval context on a single issue. The response shape is
// {class, similar_past_issues, docs, regression_prs, upstream_candidates}.
func NewBriefPreviewTool(baseURL, secret string) Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"repo": {
				"type": "string",
				"description": "Repository in owner/repo format"
			},
			"issue_number": {
				"type": "integer",
				"description": "GitHub issue number"
			},
			"hat": {
				"type": "string",
				"description": "Optional hat name from .github/hats.md to apply soft rerank boost. When omitted, retrieval returns class=other and no boost is applied."
			}
		},
		"required": ["repo", "issue_number"]
	}`)

	def := mcp.ToolDef{
		Name:        "get_brief_preview",
		Description: "Returns hat-aware retrieval bundle for a single issue: class, similar past issues, relevant docs, regression-window PRs, and upstream candidates.",
		InputSchema: schema,
	}

	handler := func(args json.RawMessage) (any, error) {
		var params struct {
			Repo        string `json:"repo"`
			IssueNumber int    `json:"issue_number"`
			Hat         string `json:"hat,omitempty"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if params.Repo == "" {
			return nil, fmt.Errorf("repo is required")
		}
		if params.IssueNumber == 0 {
			return nil, fmt.Errorf("issue_number is required")
		}
		body, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		return postJSONWithBody(baseURL+"/brief-preview", secret, body)
	}

	return Tool{Def: def, Handler: handler}
}
