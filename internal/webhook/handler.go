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

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/agent"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/comment"
	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/mirror"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/phases"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/safety"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

const (
	maxWebhookBodySize = 25 << 20 // 25 MB
	maxCommentLength   = 65536
	triageTimeout      = 5 * time.Minute
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
	agentHandler  *agent.AgentHandler
	shadowRepos   map[string]string
	mirror        *mirror.Service
}

// New creates a new webhook Handler.
// sourceRepo overrides the repo used for data lookups (vector searches). If empty, the webhook repo is used.
// ctx is used as the parent context for background triage goroutines.
// shadowRepos maps source repos to their shadow repos for triage review and agent sessions.
func New(webhookSecret string, sourceRepo string, s *store.Store, l llm.Provider, g *gh.Client, logger *slog.Logger, ctx context.Context, shadowRepos map[string]string, mirrorSvc *mirror.Service) *Handler {
	structural := safety.NewStructuralValidator(safety.StructuralConfig{
		MaxCommentLength: maxCommentLength,
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
		shadowRepos:   shadowRepos,
		mirror:        mirrorSvc,
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

	// Handle /retriage command on source repo issues
	if strings.TrimSpace(commentBody) == "/retriage" {
		if _, ok := h.shadowRepos[repo]; ok {
			log.Info("retriage requested")
			h.handleRetriage(ctx, installationID, repo, event.Issue)
			return
		}
	}

	// Check triage session first
	handled, err := h.handleTriageComment(ctx, installationID, repo, issueNumber, commentBody)
	if err != nil {
		log.Error("handling triage comment", "error", err)
	}
	if handled {
		return
	}

	// Fall through to agent session handler
	if err := h.agentHandler.HandleComment(ctx, installationID, repo, issueNumber, commentBody, commentUser); err != nil {
		log.Error("handling agent comment", "error", err)
	}

	// Check for @mention feedback on the source repo
	h.checkMentionFeedback(ctx, repo, issueNumber, event.Comment)
}

func (h *Handler) handleTriageComment(ctx context.Context, installationID int64, shadowRepo string, shadowIssueNumber int, commentBody string) (bool, error) {
	ts, err := h.store.GetTriageSessionByShadow(ctx, shadowRepo, shadowIssueNumber)
	if err != nil {
		return false, err
	}
	if ts == nil {
		return false, nil
	}

	log := h.logger.With("repo", ts.Repo, "issue", ts.IssueNumber, "shadowRepo", shadowRepo, "shadowIssue", shadowIssueNumber)
	signal := agent.ParseApprovalSignal(commentBody)

	switch signal {
	case agent.SignalApproved:
		commentID, err := h.github.CreateComment(ctx, installationID, ts.Repo, ts.IssueNumber, ts.TriageComment)
		if err != nil {
			return true, fmt.Errorf("post triage comment publicly: %w", err)
		}
		if err := h.store.RecordBotComment(ctx, store.BotComment{
			Repo:        ts.Repo,
			IssueNumber: ts.IssueNumber,
			CommentID:   commentID,
			PhasesRun:   ts.PhasesRun,
		}); err != nil {
			log.Error("recording bot comment after promotion", "error", err)
		}
		_ = h.github.CloseIssue(ctx, installationID, shadowRepo, shadowIssueNumber)
		log.Info("triage comment promoted to public issue")
		return true, nil

	case agent.SignalReject:
		_ = h.github.CloseIssue(ctx, installationID, shadowRepo, shadowIssueNumber)
		log.Info("triage session rejected")
		return true, nil

	default:
		log.Info("ignoring non-signal comment on triage shadow issue")
		return false, nil
	}
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

	// Check if already processed (bot comment or shadow triage session)
	commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
	if err != nil {
		issueLog.Error("checking bot comment", "error", err)
		return
	}
	if commented {
		issueLog.Info("bot already commented")
		return
	}
	triaged, err := h.store.HasTriageSession(ctx, repo, issue.Number)
	if err != nil {
		issueLog.Error("checking triage session", "error", err)
		return
	}
	if triaged {
		issueLog.Info("already triaged via shadow repo")
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

	// Phase 4a: Enhancement context (enhancements only)
	if isEnhancement {
		p4a, err := phases.Phase4a(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("phase 4a failed", "error", err)
		}
		issueLog.Info("phase 4a complete", "matches", len(p4a))
		result.Phase4a = p4a
	}

	// Build triage comment
	body := comment.Build(result)
	phasesRun := collectPhasesRun(result)

	if shadowRepo, ok := h.shadowRepos[repo]; ok && body != "" {
		// Post to shadow repo for review
		const totalSections = 4
		filled := totalSections - len(result.Phase1.MissingItems)
		shadowTitle := fmt.Sprintf("[Triage] [%d/%d] #%d: %s", filled, totalSections, issue.Number, issue.Title)
		shadowBody := gh.FormatShadowIssueBody(repo, issue.Number, issue.Title, issue.Body)
		shadowNumber, err := h.github.CreateIssue(ctx, installationID, shadowRepo, shadowTitle, shadowBody)
		if err != nil {
			issueLog.Error("creating shadow triage issue", "error", err)
		} else {
			instructions := "\n\n---\n\nReply `lgtm` to post this comment publicly, or `reject` to discard."
			_, err = h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, body+instructions)
			if err != nil {
				issueLog.Error("posting triage comment on shadow issue", "error", err)
			} else {
				if err := h.store.CreateTriageSession(ctx, store.TriageSession{
					Repo:              repo,
					IssueNumber:       issue.Number,
					ShadowRepo:        shadowRepo,
					ShadowIssueNumber: shadowNumber,
					TriageComment:     body,
					PhasesRun:         phasesRun,
				}); err != nil {
					issueLog.Error("recording triage session", "error", err)
				}
				issueLog.Info("triage comment posted to shadow repo", "shadowRepo", shadowRepo, "shadowIssue", shadowNumber)
			}
		}
	} else if body != "" {
		commentID, err := h.github.CreateComment(ctx, installationID, repo, issue.Number, body)
		if err != nil {
			issueLog.Error("posting comment", "error", err)
		} else {
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
	} else {
		issueLog.Info("nothing to report in triage comment")
	}

	// Start agent session for enhancements with shadow repo
	if isEnhancement {
		if shadowRepo, ok := h.shadowRepos[repo]; ok {
			issueLog.Info("starting agent session", "shadowRepo", shadowRepo)
			if err := h.agentHandler.StartSession(ctx, installationID, repo, issue.Number, shadowRepo, issue.Title, issue.Body); err != nil {
				issueLog.Error("starting agent session", "error", err)
			}
		}
	}
}

// handleRetriage re-runs the triage pipeline for an existing issue and posts
// the result to a new shadow issue. Called when a maintainer comments /retriage.
func (h *Handler) handleRetriage(ctx context.Context, installationID int64, repo string, issue gh.IssueDetail) {
	issueLog := h.logger.With("repo", repo, "issue", issue.Number)

	// Re-upsert the issue in case the body was edited
	h.upsertIssue(ctx, repo, issue)

	dataRepo := repo
	if h.sourceRepo != "" {
		dataRepo = h.sourceRepo
	}

	isBug := hasLabel(issue.Labels, "bug")
	isEnhancement := hasLabel(issue.Labels, "enhancement")

	var result comment.TriageResult
	result.IsBug = isBug
	result.IsEnhancement = isEnhancement

	result.Phase1 = phases.Phase1(issue.Body)

	if isBug {
		p2, err := phases.Phase2(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("retriage phase 2 failed", "error", err)
		}
		result.Phase2 = p2
	}

	if isEnhancement {
		p4a, err := phases.Phase4a(ctx, h.store, h.llm, issueLog, dataRepo, issue.Title, issue.Body)
		if err != nil {
			issueLog.Error("retriage phase 4a failed", "error", err)
		}
		result.Phase4a = p4a
	}

	body := comment.Build(result)
	phasesRun := collectPhasesRun(result)

	shadowRepo, ok := h.shadowRepos[repo]
	if !ok || body == "" {
		issueLog.Info("retriage produced no output or no shadow repo configured")
		return
	}

	const totalSections = 4
	filled := totalSections - len(result.Phase1.MissingItems)
	shadowTitle := fmt.Sprintf("[Retriage] [%d/%d] #%d: %s", filled, totalSections, issue.Number, issue.Title)
	shadowBody := gh.FormatShadowIssueBody(repo, issue.Number, issue.Title, issue.Body)
	shadowNumber, err := h.github.CreateIssue(ctx, installationID, shadowRepo, shadowTitle, shadowBody)
	if err != nil {
		issueLog.Error("creating retriage shadow issue", "error", err)
		return
	}

	instructions := "\n\n---\n\nReply `lgtm` to post this comment publicly, or `reject` to discard."
	_, err = h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, body+instructions)
	if err != nil {
		issueLog.Error("posting retriage comment on shadow issue", "error", err)
		return
	}

	// Upsert the triage session so lgtm/reject still work
	if err := h.store.CreateTriageSession(ctx, store.TriageSession{
		Repo:              repo,
		IssueNumber:       issue.Number,
		ShadowRepo:        shadowRepo,
		ShadowIssueNumber: shadowNumber,
		TriageComment:     body,
		PhasesRun:         phasesRun,
	}); err != nil {
		issueLog.Error("recording retriage session", "error", err)
	}

	issueLog.Info("retriage complete, posted to shadow repo", "shadowIssue", shadowNumber)
}

func (h *Handler) handlePush(ctx context.Context, event gh.PushEvent) {
	repo := event.Repo.FullName
	log := h.logger.With("repo", repo, "ref", event.Ref)

	h.recordEvent(ctx, pushToRepoEvent(repo, event.Ref))

	shadowRepo, ok := h.shadowRepos[repo]
	if !ok {
		log.Info("no shadow repo configured, skipping mirror sync")
		return
	}

	if h.mirror == nil {
		log.Warn("mirror service not configured, skipping sync")
		return
	}

	log.Info("triggering mirror sync for push event")
	if err := h.mirror.Sync(ctx, event.Installation.ID, repo, shadowRepo); err != nil {
		log.Error("mirror sync failed", "error", err)
		return
	}
	log.Info("mirror sync completed successfully")
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

	// Only track edits on issues where the bot has posted a public comment.
	// Shadow-only triage sessions don't count — users haven't seen the bot's feedback yet.
	commented, err := h.store.HasBotCommented(ctx, repo, issue.Number)
	if err != nil {
		log.Error("checking bot comment for edit feedback", "error", err)
		return
	}
	if !commented {
		log.Debug("edited issue has no public bot comment, skipping feedback check")
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

	// Only record if the bot has posted a public comment on this issue
	commented, err := h.store.HasBotCommented(ctx, repo, issueNumber)
	if err != nil {
		log.Error("checking bot comment for mention feedback", "error", err)
		return
	}
	if !commented {
		return
	}

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
	if r.Phase4a != nil {
		phases = append(phases, "phase4a")
	}
	return phases
}
