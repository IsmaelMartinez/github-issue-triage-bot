package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

type learningRequest struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	Kind        string `json:"kind"`
	Hat         string `json:"hat,omitempty"`
	Draft       string `json:"draft,omitempty"`
	Final       string `json:"final,omitempty"`
	DiffSummary string `json:"diff_summary"`
}

func (srv *server) learningHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req learningRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Repo == "" || req.IssueNumber == 0 || req.DiffSummary == "" {
		http.Error(w, "repo, issue_number, and diff_summary required", http.StatusBadRequest)
		return
	}
	if !srv.allowedRepos[req.Repo] {
		http.Error(w, "repo not in allow-list", http.StatusForbidden)
		return
	}

	title := fmt.Sprintf("learning/%d/%s", req.IssueNumber, req.Kind)
	embeddingText := req.DiffSummary
	if req.Final != "" {
		summary := req.Final
		if len(summary) > 200 {
			summary = summary[:200]
		}
		embeddingText += "\n\nIssue context: " + summary
	}

	vec, err := srv.llm.Embed(r.Context(), embeddingText)
	if err != nil {
		http.Error(w, fmt.Sprintf("embed: %v", err), http.StatusInternalServerError)
		return
	}

	doc := store.Document{
		Repo:    req.Repo,
		DocType: store.DocTypeLearning,
		Title:   title,
		Content: req.DiffSummary,
		Metadata: map[string]any{
			"issue_number": req.IssueNumber,
			"kind":         req.Kind,
			"captured_at":  time.Now().UTC().Format(time.RFC3339),
		},
		Embedding: vec,
	}
	if req.Hat != "" {
		doc.Metadata["hat"] = req.Hat
	}
	if req.Draft != "" {
		draft := req.Draft
		if len(draft) > 200 {
			draft = draft[:200]
		}
		doc.Metadata["draft_summary"] = draft
	}
	if req.Final != "" {
		final := req.Final
		if len(final) > 200 {
			final = final[:200]
		}
		doc.Metadata["final_summary"] = final
	}

	if err := srv.store.UpsertDocument(r.Context(), doc); err != nil {
		http.Error(w, fmt.Sprintf("upsert: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"title":%q}`, title)
}
