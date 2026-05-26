package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/agent"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/config"
	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/safety"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

const (
	maxWebhookBodySize = 2 << 20 // 2 MB — issue/comment events are <100 KB; push events rarely exceed 1 MB
	maxCommentLength   = 65536
	triageTimeout      = 5 * time.Minute
)

// recoverGoroutine logs panics in background goroutines instead of crashing.
func recoverGoroutine(logger *slog.Logger, name string) {
	if r := recover(); r != nil {
		logger.Error("panic in background goroutine", "goroutine", name, "error", r)
	}
}

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
	agentHandler *agent.AgentHandler
	structural   *safety.StructuralValidator
	configCaches map[string]*config.Cache
	configMu     sync.Mutex
}

// New creates a new webhook Handler.
// sourceRepo overrides the repo used for data lookups (vector searches). If empty, the webhook repo is used.
// ctx is used as the parent context for background triage goroutines.
func New(webhookSecret string, sourceRepo string, s *store.Store, l llm.Provider, g *gh.Client, logger *slog.Logger, ctx context.Context) *Handler {
	structural := safety.NewStructuralValidator(safety.StructuralConfig{
		MaxCommentLength: maxCommentLength,
		AllowedURLHosts: []string{
			"github.com",
			"ismaelmartinez.github.io",
			"teams.microsoft.com",
			"feedbackportal.microsoft.com",
			"learn.microsoft.com",
			"electronjs.org",
			"www.electronjs.org",
			"releases.electronjs.org",
		},
		AllowedMentions: []string{"ismael-triage-bot"},
	})
	llmSafety := safety.NewLLMValidator(l)
	agentHandler := agent.NewAgentHandler(s, l, g, structural, llmSafety, logger)

	return &Handler{
		webhookSecret: webhookSecret,
		sourceRepo:    sourceRepo,
		store:         s,
		llm:           l,
		github:        g,
		logger:        logger,
		ctx:           ctx,
		agentHandler:  agentHandler,
		structural:    structural,
		configCaches:  make(map[string]*config.Cache),
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

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
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
	if deliveryID == "" {
		http.Error(w, "missing X-GitHub-Delivery header", http.StatusBadRequest)
		return
	}
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

	eventType := r.Header.Get("X-GitHub-Event")

	switch eventType {
	case "issues":
		var event gh.IssueEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer recoverGoroutine(h.logger, "processEvent")
			ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
			defer cancel()
			h.processEvent(ctx, event)
		}()

	case "issue_comment":
		var event gh.IssueCommentEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Only handle new comments, not edits or deletions
		if event.Action != "created" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ignored comment action")
			return
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer recoverGoroutine(h.logger, "processCommentEvent")
			ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
			defer cancel()
			h.processCommentEvent(ctx, event)
		}()

	case "push":
		var event gh.PushEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Only mirror pushes to the default branch
		if event.Ref == "refs/heads/main" || event.Ref == "refs/heads/master" {
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				defer recoverGoroutine(h.logger, "handlePush")
				ctx, cancel := context.WithTimeout(h.ctx, triageTimeout)
				defer cancel()
				h.handlePush(ctx, event)
			}()
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ignored non-default branch push")
			return
		}

	default:
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ignored event type")
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "accepted")
}

func (h *Handler) processCommentEvent(ctx context.Context, event gh.IssueCommentEvent) {
	repo := event.Repo.FullName
	commentUser := event.Comment.User.Login
	commentBody := event.Comment.Body
	issueNumber := event.Issue.Number
	installationID := event.Installation.ID

	if event.Comment.User.Type == "Bot" {
		return
	}

	h.recordEvent(ctx, commentToRepoEvent(repo, issueNumber, commentUser, commentBody))

	log := h.logger.With("repo", repo, "issue", issueNumber, "commentUser", commentUser)
	log.Info("processing comment event")

	// Handle /pause and /unpause commands (repo owner only)
	trimmed := strings.TrimSpace(commentBody)
	if trimmed == "/pause" || trimmed == "/unpause" {
		// Only allow the repo owner to pause/unpause
		owner := strings.SplitN(repo, "/", 2)[0]
		if !strings.EqualFold(commentUser, owner) {
			log.Info("ignoring pause command from non-owner", "user", commentUser, "owner", owner)
			return
		}
		paused := trimmed == "/pause"
		if err := h.store.SetPaused(ctx, repo, paused, commentUser); err != nil {
			log.Error("setting pause state", "error", err)
		} else {
			state := "paused"
			if !paused {
				state = "unpaused"
			}
			msg := fmt.Sprintf("Bot %s for `%s` by @%s.", state, repo, commentUser)
			_, _ = h.github.CreateComment(ctx, installationID, repo, issueNumber, msg)
			log.Info("bot pause state changed", "paused", paused, "by", commentUser)
		}
		return
	}

	// Fall through to agent session handler
	if err := h.agentHandler.HandleComment(ctx, installationID, repo, issueNumber, commentBody, commentUser); err != nil {
		log.Error("handling agent comment", "error", err)
	}

	// Check for @mention feedback on the source repo
	h.checkMentionFeedback(ctx, repo, issueNumber, event.Comment)
}

func (h *Handler) processEvent(ctx context.Context, event gh.IssueEvent) {
	repo := event.Repo.FullName
	issue := event.Issue
	installationID := event.Installation.ID

	h.recordEvent(ctx, issueToRepoEvent(repo, event.Action, issue))

	switch event.Action {
	case "opened":
		h.handleOpened(ctx, installationID, repo, issue)
	case "closed", "reopened":
		h.handleStateChange(ctx, repo, issue)
	case "edited":
		h.handleEdited(ctx, installationID, repo, issue, event.Changes)
	case "labeled", "unlabeled":
		h.handleLabelChange(ctx, repo, issue)
	default:
		h.logger.Info("ignoring action", "action", event.Action, "issue", issue.Number)
	}
}

// handleLabelChange syncs the issue's labels column when GitHub emits a
// labeled or unlabeled event. The full current label set comes through on
// every label event, so we replace rather than diff. No re-embedding: label
// edits do not affect the title/summary the embedding is computed from.
//
// When the bot has previously triaged the issue, the handler also checks for
// meaningful label corrections (e.g. bug removed, enhancement toggled) and
// captures them as triage_learning documents for RAG enrichment.
func (h *Handler) handleLabelChange(ctx context.Context, repo string, issue gh.IssueDetail) {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	if err := h.store.UpdateIssueLabels(ctx, repo, issue.Number, labels); err != nil {
		h.logger.Error("updating issue labels", "repo", repo, "issue", issue.Number, "error", err)
	}

	// Capture label corrections as learnings for issues the bot triaged.
	h.captureLabelLearning(ctx, repo, issue, labels)
}

// classificationLabels are the labels whose addition or removal signals a
// meaningful classification change worth capturing as a triage learning.
var classificationLabels = map[string]bool{
	"bug":         true,
	"enhancement": true,
	"question":    true,
	"blocked":     true,
	"wontfix":     true,
	"invalid":     true,
	"duplicate":   true,
}

// captureLabelLearning checks whether a label change on a bot-triaged issue
// represents a correction, and if so upserts a triage_learning document.
func (h *Handler) captureLabelLearning(ctx context.Context, repo string, issue gh.IssueDetail, currentLabels []string) {
	log := h.logger.With("repo", repo, "issue", issue.Number)

	commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
	if err != nil {
		log.Error("checking bot comment for learning", "error", err)
		return
	}
	if !commented {
		return
	}

	// Build current classification label set.
	var classLabels []string
	for _, l := range currentLabels {
		if classificationLabels[l] {
			classLabels = append(classLabels, l)
		}
	}

	// A correction worth capturing: the issue has classification labels that
	// differ from what would be expected. Since we don't store the original
	// bot classification, any non-trivial classification label set on a
	// bot-triaged issue is worth recording — the maintainer's chosen labels
	// become ground truth for future retrieval.
	if len(classLabels) == 0 {
		return
	}

	diffSummary := fmt.Sprintf(
		"Issue #%d (%s) — maintainer set classification labels: [%s]. Title: %s",
		issue.Number,
		repo,
		strings.Join(classLabels, ", "),
		issue.Title,
	)

	embedding, err := h.llm.Embed(ctx, diffSummary)
	if err != nil {
		log.Error("embedding label learning", "error", err)
		return
	}

	title := fmt.Sprintf("learning/%d/label_correction", issue.Number)
	doc := store.Document{
		Repo:    repo,
		DocType: store.DocTypeLearning,
		Title:   title,
		Content: diffSummary,
		Metadata: map[string]any{
			"issue_number": issue.Number,
			"kind":         "label_correction",
			"labels":       classLabels,
			"captured_at":  time.Now().UTC().Format(time.RFC3339),
		},
		Embedding: embedding,
	}
	if err := h.store.UpsertDocument(ctx, doc); err != nil {
		log.Error("upserting label learning", "error", err)
		return
	}
	log.Info("captured label correction learning", "labels", classLabels)
}

func (h *Handler) handleOpened(ctx context.Context, installationID int64, repo string, issue gh.IssueDetail) {
	issueLog := h.logger.With("repo", repo, "issue", issue.Number)
	issueLog.Info("processing new issue")

	// Check pause status
	paused, err := h.store.IsPaused(ctx, repo)
	if err != nil {
		issueLog.Error("checking pause status", "error", err)
	}
	if paused {
		issueLog.Info("bot is paused for this repo, skipping")
		return
	}

	// Check butler.json kill switch and capabilities
	cfg := h.getConfig(ctx, installationID, repo)
	if !cfg.IsEnabled() {
		issueLog.Info("bot is disabled via butler.json, skipping")
		return
	}

	// Register the project's docs URL host with the safety validator so
	// LLM-generated links to project documentation are not rejected.
	if cfg.Project.DocsURL != "" {
		if u, err := url.Parse(cfg.Project.DocsURL); err == nil && u.Hostname() != "" {
			h.structural.AllowHost(u.Hostname())
		}
	}

	// Set LLM daily limit from config
	if llmClient, ok := h.llm.(*llm.Client); ok {
		llmClient.SetDailyLimit(cfg.MaxDailyLLMCalls)
	}

	// Skip bot accounts
	if strings.Contains(issue.User.Login, "[bot]") || strings.HasSuffix(issue.User.Login, "-bot") {
		issueLog.Info("skipping bot account", "user", issue.User.Login)
		return
	}

	// Silent RAG mode: embed the issue for retrieval via /brief-preview.
	h.upsertIssue(ctx, repo, issue)
	issueLog.Info("issue embedded for RAG retrieval")
}

func (h *Handler) getConfig(ctx context.Context, installationID int64, repo string) config.ButlerConfig {
	h.configMu.Lock()
	cache, ok := h.configCaches[repo]
	if !ok {
		cache = config.NewCache(1*time.Hour, func() ([]byte, error) {
			fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return h.github.GetFileContents(fetchCtx, installationID, repo, ".github/butler.json")
		})
		h.configCaches[repo] = cache
	}
	h.configMu.Unlock()

	cfg, _ := cache.Get()
	return cfg
}

func (h *Handler) handlePush(ctx context.Context, event gh.PushEvent) {
	repo := event.Repo.FullName
	log := h.logger.With("repo", repo, "ref", event.Ref)

	h.recordEvent(ctx, pushToRepoEvent(repo, event.Ref))

	cfg := h.getConfig(ctx, event.Installation.ID, repo)
	if cfg.Capabilities.AutoIngest {
		log.Info("auto-ingesting docs from push")
		h.autoIngestDocs(ctx, event.Installation.ID, repo, event.Commits, cfg.DocPaths)
	}
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

const botMentionHandle = "@ismael-triage-bot"

func (h *Handler) handleEdited(ctx context.Context, installationID int64, repo string, issue gh.IssueDetail, changes *gh.IssueChanges) {
	log := h.logger.With("repo", repo, "issue", issue.Number)

	// Always update the issue embedding with the new body
	h.upsertIssue(ctx, repo, issue)

	// No body change to analyze (could be title or label edit)
	if changes == nil || changes.Body == nil {
		log.Debug("edited event without body change, skipping feedback check")
		return
	}

	// Only track edit signals for bugs (Phase 1 is only shown to users for bugs)
	if !hasLabel(issue.Labels, "bug") {
		log.Debug("edited event on non-bug issue, skipping feedback check")
		return
	}

	filled := computeFilledSections(changes.Body.From, issue.Body)
	if len(filled) == 0 {
		log.Debug("edit did not fill any missing sections")
		return
	}

	oldResult := phases.Phase1(changes.Body.From)
	newResult := phases.Phase1(issue.Body)
	if err := h.store.RecordFeedbackSignal(ctx, store.FeedbackSignal{
		Repo:        repo,
		IssueNumber: issue.Number,
		SignalType:  "issue_edit_filled",
		Details: map[string]any{
			"filled_items":  filled,
			"total_flagged": len(oldResult.MissingItems),
			"remaining":     len(newResult.MissingItems),
		},
	}); err != nil {
		log.Error("recording edit feedback signal", "error", err)
		return
	}
	log.Info("recorded edit fill signal", "filled", filled)
}

func (h *Handler) checkMentionFeedback(ctx context.Context, repo string, issueNumber int, comment gh.CommentDetail) {
	if !strings.Contains(comment.Body, botMentionHandle) {
		return
	}

	log := h.logger.With("repo", repo, "issue", issueNumber)

	body := comment.Body
	if len(body) > 500 {
		cut := 500
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
		body = body[:cut]
	}

	if err := h.store.RecordFeedbackSignal(ctx, store.FeedbackSignal{
		Repo:        repo,
		IssueNumber: issueNumber,
		SignalType:  "user_mention",
		Details: map[string]any{
			"comment_id": comment.ID,
			"body":       body,
			"user":       comment.User.Login,
		},
	}); err != nil {
		log.Error("recording mention feedback signal", "error", err)
		return
	}
	log.Info("recorded mention feedback signal", "user", comment.User.Login)
}

// computeFilledSections returns the labels of Phase 1 missing items that were
// present in oldBody but are no longer missing in newBody.
func computeFilledSections(oldBody, newBody string) []string {
	oldResult := phases.Phase1(oldBody)
	newResult := phases.Phase1(newBody)

	newMissing := make(map[string]bool, len(newResult.MissingItems))
	for _, item := range newResult.MissingItems {
		newMissing[item.Label] = true
	}

	var filled []string
	for _, item := range oldResult.MissingItems {
		if !newMissing[item.Label] {
			filled = append(filled, item.Label)
		}
	}
	return filled
}

// isDocumentationBug detects bug reports about docs, website, or meta issues
// where debug logs and PWA reproducibility are irrelevant.
func isDocumentationBug(title string) bool {
	lower := strings.ToLower(title)
	keywords := []string{"documentation", "docs", "readme", "broken link", "typo", "website", "changelog", "contributing"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
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

