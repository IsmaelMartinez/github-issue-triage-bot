package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/comment"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// Handler processes GitHub webhook events.
type Handler struct {
	webhookSecret string
	sourceRepo    string
	store         *store.Store
	llm           llm.Provider
	github        *gh.Client
	logger        *slog.Logger
	wg            sync.WaitGroup
	ctx           context.Context
}

// New creates a new webhook Handler.
// sourceRepo overrides the repo used for data lookups (vector searches). If empty, the webhook repo is used.
// ctx is used as the parent context for background triage goroutines.
func New(webhookSecret string, sourceRepo string, s *store.Store, l llm.Provider, g *gh.Client, logger *slog.Logger, ctx context.Context) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		sourceRepo:    sourceRepo,
		store:         s,
		llm:           l,
		github:        g,
		logger:        logger,
		ctx:           ctx,
	}
}

// Wait blocks until all in-flight triage goroutines have completed.
func (h *Handler) Wait() {
	h.wg.Wait()
}

// ServeHTTP handles incoming webhook POST requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25<<20) // 25 MB
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

	// Reject duplicate deliveries
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID != "" {
		duplicate, err := h.store.CheckAndRecordDelivery(r.Context(), deliveryID)
		if err != nil {
			h.logger.Error("checking delivery ID", "error", err)
			http.Error(w, "dedup check failed", http.StatusInternalServerError)
			return
		}
		if duplicate {
			h.logger.Info("duplicate delivery rejected", "deliveryID", deliveryID)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "duplicate delivery")
			return
		}
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
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		ctx, cancel := context.WithTimeout(h.ctx, 5*time.Minute)
		defer cancel()
		h.processEvent(ctx, event)
	}()

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "accepted")
}

func (h *Handler) processEvent(ctx context.Context, event gh.IssueEvent) {
	repo := event.Repo.FullName
	issue := event.Issue
	installationID := event.Installation.ID

	switch event.Action {
	case "opened":
		h.handleOpened(ctx, installationID, repo, issue)
	case "closed", "reopened":
		h.handleStateChange(ctx, repo, issue)
	default:
		h.logger.Info("ignoring action", "action", event.Action, "issue", issue.Number)
	}
}

func (h *Handler) handleOpened(ctx context.Context, installationID int64, repo string, issue gh.IssueDetail) {
	issueLog := h.logger.With("repo", repo, "issue", issue.Number)
	issueLog.Info("processing new issue")

	// Skip bot accounts
	if strings.Contains(issue.User.Login, "[bot]") || strings.HasSuffix(issue.User.Login, "-bot") {
		issueLog.Info("skipping bot account", "user", issue.User.Login)
		return
	}

	// Check if bot already commented
	commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
	if err != nil {
		issueLog.Error("checking bot comment", "error", err)
		return
	}
	if commented {
		issueLog.Info("bot already commented")
		return
	}

	// Update issue in database under the webhook repo (after bot/duplicate checks to avoid wasting an embedding call)
	h.upsertIssue(ctx, repo, issue)

	// Use sourceRepo for data lookups (vector searches), falling back to webhook repo
	dataRepo := repo
	if h.sourceRepo != "" {
		dataRepo = h.sourceRepo
		issueLog.Info("using source repo for data lookups", "dataRepo", dataRepo)
	}

	// Determine issue type
	isBug := hasLabel(issue.Labels, "bug")
	isEnhancement := hasLabel(issue.Labels, "enhancement")
	issueLog.Info("issue classification", "isBug", isBug, "isEnhancement", isEnhancement, "labelCount", len(issue.Labels))

	// Run phases
	var result comment.TriageResult
	result.IsBug = isBug
	result.IsEnhancement = isEnhancement

	// Phase 1: Missing info (always runs)
	result.Phase1 = phases.Phase1(issue.Body)

	// Phase 2: Solution suggestions (bugs only)
	if isBug {
		p2, err := phases.Phase2(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("phase 2 failed", "error", err)
		}
		issueLog.Info("phase 2 complete", "suggestions", len(p2))
		result.Phase2 = p2
	}

	// Phase 3: Duplicate detection (bugs only)
	if isBug {
		p3, err := phases.Phase3(ctx, h.store, h.llm, issueLog, dataRepo, issue.Number, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("phase 3 failed", "error", err)
		}
		issueLog.Info("phase 3 complete", "duplicates", len(p3))
		result.Phase3 = p3
	}

	// Phase 4a: Enhancement context (enhancements only)
	if isEnhancement {
		p4a, err := phases.Phase4a(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("phase 4a failed", "error", err)
		}
		issueLog.Info("phase 4a complete", "matches", len(p4a))
		result.Phase4a = p4a
	}

	// Phase 4b: Misclassification detection (always runs)
	currentLabel := "bug"
	if isEnhancement {
		currentLabel = "enhancement"
	}
	p4b, err := phases.Phase4b(ctx, h.llm, issueLog, issue.Title, issue.Body, currentLabel)
	if err != nil {
		issueLog.Error("phase 4b failed", "error", err)
	}
	issueLog.Info("phase 4b complete", "result", p4b)
	result.Phase4b = p4b

	// Build comment
	body := comment.Build(result)
	if body == "" {
		issueLog.Info("nothing to report")
		return
	}

	// Post comment
	commentID, err := h.github.CreateComment(ctx, installationID, repo, issue.Number, body)
	if err != nil {
		issueLog.Error("posting comment", "error", err)
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
		issueLog.Error("recording bot comment", "error", err)
	}

	issueLog.Info("comment posted", "phases", phasesRun)
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
		// Walk back from the cut point to avoid splitting a multi-byte UTF-8 rune
		for maxLen > 0 && !utf8.RuneStart(result[maxLen]) {
			maxLen--
		}
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
