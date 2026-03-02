package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/comment"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Handler processes GitHub webhook events.
type Handler struct {
	webhookSecret string
	store         *store.Store
	llm           *llm.Client
	github        *gh.Client
	logger        *slog.Logger
}

// New creates a new webhook Handler.
func New(webhookSecret string, s *store.Store, l *llm.Client, g *gh.Client, logger *slog.Logger) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		store:         s,
		llm:           l,
		github:        g,
		logger:        logger,
	}
}

// ServeHTTP handles incoming webhook POST requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify webhook signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if !gh.VerifyWebhookSignature(body, sig, h.webhookSecret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Only handle issue events
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType != "issues" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ignored event type")
		return
	}

	var event gh.IssueEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Handle asynchronously so we respond to GitHub quickly
	go h.processEvent(context.Background(), event)

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "accepted")
}

func (h *Handler) processEvent(ctx context.Context, event gh.IssueEvent) {
	repo := event.Repo.FullName
	issue := event.Issue

	switch event.Action {
	case "opened":
		h.handleOpened(ctx, repo, issue)
	case "closed", "reopened":
		h.handleStateChange(ctx, repo, issue)
	default:
		h.logger.Info("ignoring action", "action", event.Action, "issue", issue.Number)
	}
}

func (h *Handler) handleOpened(ctx context.Context, repo string, issue gh.IssueDetail) {
	h.logger.Info("processing new issue", "repo", repo, "issue", issue.Number)

	// Update issue in database
	h.upsertIssue(ctx, repo, issue)

	// Skip bot accounts
	if strings.Contains(issue.User.Login, "[bot]") || strings.HasSuffix(issue.User.Login, "-bot") {
		h.logger.Info("skipping bot account", "user", issue.User.Login)
		return
	}

	// Check if bot already commented
	commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
	if err != nil {
		h.logger.Error("checking bot comment", "error", err)
		return
	}
	if commented {
		h.logger.Info("bot already commented", "issue", issue.Number)
		return
	}

	// Determine issue type
	isBug := hasLabel(issue.Labels, "bug")
	isEnhancement := hasLabel(issue.Labels, "enhancement")

	// Run phases
	var result comment.TriageResult
	result.IsBug = isBug
	result.IsEnhancement = isEnhancement

	// Phase 1: Missing info (always runs)
	result.Phase1 = phases.Phase1(issue.Body)

	// Phase 2: Solution suggestions (bugs only)
	if isBug {
		p2, err := phases.Phase2(ctx, h.store, h.llm, repo, issue.Title, issue.Body)
		if err != nil {
			h.logger.Error("phase 2 failed", "error", err)
		}
		result.Phase2 = p2
	}

	// Phase 3: Duplicate detection (bugs only)
	if isBug {
		p3, err := phases.Phase3(ctx, h.store, h.llm, repo, issue.Number, issue.Title, issue.Body)
		if err != nil {
			h.logger.Error("phase 3 failed", "error", err)
		}
		result.Phase3 = p3
	}

	// Phase 4a: Enhancement context (enhancements only)
	if isEnhancement {
		p4a, err := phases.Phase4a(ctx, h.store, h.llm, repo, issue.Title, issue.Body)
		if err != nil {
			h.logger.Error("phase 4a failed", "error", err)
		}
		result.Phase4a = p4a
	}

	// Phase 4b: Misclassification detection (always runs)
	currentLabel := "bug"
	if isEnhancement {
		currentLabel = "enhancement"
	}
	p4b, err := phases.Phase4b(ctx, h.llm, issue.Title, issue.Body, currentLabel)
	if err != nil {
		h.logger.Error("phase 4b failed", "error", err)
	}
	result.Phase4b = p4b

	// Build comment
	body := comment.Build(result)
	if body == "" {
		h.logger.Info("nothing to report", "issue", issue.Number)
		return
	}

	// Post comment
	commentID, err := h.github.CreateComment(ctx, repo, issue.Number, body)
	if err != nil {
		h.logger.Error("posting comment", "error", err)
		return
	}

	// Record bot comment
	phasesRun := collectPhasesRun(result)
	if err := h.store.RecordBotComment(ctx, store.BotComment{
		Repo:        repo,
		IssueNumber: issue.Number,
		CommentID:   commentID,
		PhasesRun:   phasesRun,
	}); err != nil {
		h.logger.Error("recording bot comment", "error", err)
	}

	h.logger.Info("comment posted", "issue", issue.Number, "phases", phasesRun)
}

func (h *Handler) handleStateChange(ctx context.Context, repo string, issue gh.IssueDetail) {
	h.logger.Info("updating issue state", "repo", repo, "issue", issue.Number, "state", issue.State)
	h.upsertIssue(ctx, repo, issue)
}

func (h *Handler) upsertIssue(ctx context.Context, repo string, issue gh.IssueDetail) {
	summary := sanitizeBody(issue.Body, 200)
	text := fmt.Sprintf("%s\n%s", issue.Title, summary)

	embedding, err := h.llm.Embed(ctx, text)
	if err != nil {
		h.logger.Error("embedding issue", "error", err)
		return
	}

	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}

	if err := h.store.UpsertIssue(ctx, store.Issue{
		Repo:      repo,
		Number:    issue.Number,
		Title:     issue.Title,
		Summary:   summary,
		State:     issue.State,
		Labels:    labels,
		Embedding: embedding,
	}); err != nil {
		h.logger.Error("upserting issue", "error", err)
	}
}

func hasLabel(labels []gh.LabelInfo, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

func sanitizeBody(body string, maxLen int) string {
	// Remove code fences
	result := body
	for {
		start := strings.Index(result, "```")
		if start == -1 {
			break
		}
		end := strings.Index(result[start+3:], "```")
		if end == -1 {
			result = result[:start]
			break
		}
		result = result[:start] + result[start+3+end+3:]
	}

	// Remove HTML tags
	for {
		start := strings.Index(result, "<")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}

	result = strings.TrimSpace(result)
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

func collectPhasesRun(r comment.TriageResult) []string {
	var phases []string
	phases = append(phases, "phase1")
	if r.Phase2 != nil {
		phases = append(phases, "phase2")
	}
	if r.Phase3 != nil {
		phases = append(phases, "phase3")
	}
	if r.Phase4a != nil {
		phases = append(phases, "phase4a")
	}
	if r.Phase4b != nil {
		phases = append(phases, "phase4b")
	}
	return phases
}
