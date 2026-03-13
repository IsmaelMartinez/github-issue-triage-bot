package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/llm"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/safety"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// AgentHandler manages the enhancement research agent lifecycle,
// coordinating between shadow repos, LLM analysis, and human review.
type AgentHandler struct {
	store      *store.Store
	llm        llm.Provider
	github     *gh.Client
	structural *safety.StructuralValidator
	llmSafety  *safety.LLMValidator
	logger     *slog.Logger
}

// NewAgentHandler creates a new AgentHandler with all required dependencies.
func NewAgentHandler(s *store.Store, l llm.Provider, g *gh.Client, structural *safety.StructuralValidator, llmSafety *safety.LLMValidator, logger *slog.Logger) *AgentHandler {
	return &AgentHandler{
		store:      s,
		llm:        l,
		github:     g,
		structural: structural,
		llmSafety:  llmSafety,
		logger:     logger,
	}
}

// StartSession creates a mirror issue in the shadow repo and posts a context
// brief summarising related documents and issues. Maintainers can then reply
// with "research" to trigger full synthesis, or "reject" to close the session.
func (h *AgentHandler) StartSession(ctx context.Context, installationID int64, sourceRepo string, issueNumber int, shadowRepo string, title, body string) error {
	log := h.logger.With("sourceRepo", sourceRepo, "issue", issueNumber, "shadowRepo", shadowRepo)

	// Create mirror issue in shadow repo
	shadowBody := gh.FormatShadowIssueBody(sourceRepo, issueNumber, title, body)
	shadowNumber, err := h.github.CreateIssue(ctx, installationID, shadowRepo, "[Research] "+title, shadowBody)
	if err != nil {
		return fmt.Errorf("create shadow issue: %w", err)
	}
	log = log.With("shadowIssue", shadowNumber)
	log.Info("created shadow issue")

	// Run the rest of the session setup, posting error feedback on the shadow
	// issue if anything fails after issue creation.
	if err := h.startSessionAfterCreate(ctx, installationID, sourceRepo, issueNumber, shadowRepo, shadowNumber, title, body, log); err != nil {
		errMsg := fmt.Sprintf("Something went wrong setting up the context brief. A maintainer can trigger a retry with `/retriage`.\n\n<details><summary>Error details</summary>\n\n```\n%s\n```\n</details>", err.Error())
		if _, postErr := h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, errMsg); postErr != nil {
			log.Error("failed to post error feedback on shadow issue", "error", postErr)
		}
		return err
	}
	return nil
}

// retryContextBrief retries posting a context brief for a session stuck in StageNew.
func (h *AgentHandler) retryContextBrief(ctx context.Context, installationID int64, sess *store.AgentSession, title, body string, log *slog.Logger) error {
	return h.buildAndPostContextBrief(ctx, installationID, sess.ID, sess.Repo, sess.IssueNumber, sess.ShadowRepo, sess.ShadowIssueNumber, title, body, log)
}

func (h *AgentHandler) startSessionAfterCreate(ctx context.Context, installationID int64, sourceRepo string, issueNumber int, shadowRepo string, shadowNumber int, title, body string, log *slog.Logger) error {
	// Create session
	sessionID, err := h.store.CreateSession(ctx, store.AgentSession{
		Repo:              sourceRepo,
		IssueNumber:       issueNumber,
		ShadowRepo:        shadowRepo,
		ShadowIssueNumber: shadowNumber,
		Stage:             store.StageNew,
		Context:           map[string]any{"title": title, "body": body},
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return h.buildAndPostContextBrief(ctx, installationID, sessionID, sourceRepo, issueNumber, shadowRepo, shadowNumber, title, body, log)
}

func (h *AgentHandler) buildAndPostContextBrief(ctx context.Context, installationID, sessionID int64, sourceRepo string, issueNumber int, shadowRepo string, shadowNumber int, title, body string, log *slog.Logger) error {
	log = log.With("sessionID", sessionID)

	// Embed title+body for vector search
	embedding, err := h.llm.Embed(ctx, fmt.Sprintf("%s\n%s", title, body))
	if err != nil {
		return fmt.Errorf("embed issue: %w", err)
	}

	// Search for similar documents (non-fatal)
	similarDocs, err := h.store.FindSimilarDocuments(ctx, sourceRepo, store.EnhancementDocTypes, embedding, 5)
	if err != nil {
		log.Error("find similar documents", "error", err)
	}

	// Search for similar issues (non-fatal)
	similarIssues, err := h.store.FindSimilarIssues(ctx, sourceRepo, embedding, issueNumber, 5)
	if err != nil {
		log.Error("find similar issues", "error", err)
	}

	// Build context brief
	brief, err := BuildContextBrief(ctx, h.llm, title, body, similarDocs, similarIssues, sourceRepo, issueNumber)
	if err != nil {
		return fmt.Errorf("build context brief: %w", err)
	}

	// Format as markdown
	briefMD := FormatContextBriefMarkdown(brief)

	// Run structural safety check
	structResult := h.structural.Validate(briefMD)
	if !structResult.Passed {
		log.Error("structural safety check failed for context brief", "reason", structResult.Reason)
		return fmt.Errorf("structural safety check failed: %s", structResult.Reason)
	}

	// Skip LLM safety check for context briefs: they are posted to the private
	// shadow repo and intentionally include diverse vector search results that
	// may not all be directly relevant. The structural check above is sufficient.

	// Post context brief on shadow issue
	if _, err := h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, briefMD); err != nil {
		return fmt.Errorf("post context brief: %w", err)
	}

	// Update session to context_brief stage
	if err := h.store.UpdateSessionStage(ctx, sessionID, store.StageContextBrief, map[string]any{"title": title, "body": body}, 0); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Create audit entry
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sessionID,
		ActionType:        "posted_context_brief",
		InputHash:         hashString(title + body),
		OutputSummary:     truncate(briefMD, 200),
		SafetyCheckPassed: true,
		ConfidenceScore:   1.0, // structural check only
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	log.Info("posted context brief", "docs", len(similarDocs), "issues", len(similarIssues))
	return nil
}

func (h *AgentHandler) startResearch(ctx context.Context, installationID, sessionID int64, shadowRepo string, shadowNumber int, sourceRepo string, issueNumber int, title, body string, clarificationAnswers []string, roundTrips ...int) error {
	// Preserve round-trip count if provided (default 0 for initial research)
	currentRoundTrips := 0
	if len(roundTrips) > 0 {
		currentRoundTrips = roundTrips[0]
	}
	log := h.logger.With("sessionID", sessionID, "shadowRepo", shadowRepo, "shadowIssue", shadowNumber)

	// Embed title+body for vector search
	embedding, err := h.llm.Embed(ctx, fmt.Sprintf("%s\n%s", title, body))
	if err != nil {
		return fmt.Errorf("embed issue: %w", err)
	}

	// Search for similar docs
	similarDocs, err := h.store.FindSimilarDocuments(ctx, sourceRepo, store.EnhancementDocTypes, embedding, 5)
	if err != nil {
		log.Error("find similar documents", "error", err)
	}
	var docSummaries []string
	for _, d := range similarDocs {
		docSummaries = append(docSummaries, fmt.Sprintf("[%s] %s: %s", d.DocType, d.Title, truncate(d.Content, 500)))
	}

	// Search for similar issues
	similarIssues, err := h.store.FindSimilarIssues(ctx, sourceRepo, embedding, issueNumber, 5)
	if err != nil {
		log.Error("find similar issues", "error", err)
	}
	var issueSummaries []string
	for _, i := range similarIssues {
		issueSummaries = append(issueSummaries, fmt.Sprintf("#%d %s: %s", i.Number, i.Title, truncate(i.Summary, 300)))
	}

	// Enrich body with clarification answers if present
	researchBody := body
	if len(clarificationAnswers) > 0 {
		researchBody = body + "\n\n--- Clarification Answers ---\n" + strings.Join(clarificationAnswers, "\n")
	}

	// Synthesize research
	doc, err := SynthesizeResearch(ctx, h.llm, title, researchBody, docSummaries, issueSummaries)
	if err != nil {
		return fmt.Errorf("synthesize research: %w", err)
	}

	// Format as markdown
	researchMD := FormatResearchMarkdown(doc, sourceRepo, issueNumber)

	// Run structural safety check
	structResult := h.structural.Validate(researchMD)
	if !structResult.Passed {
		log.Error("structural safety check failed for research", "reason", structResult.Reason)
		return fmt.Errorf("structural safety check failed: %s", structResult.Reason)
	}

	// Run LLM safety check
	issueContext := fmt.Sprintf("Enhancement: %s\n\n%s", title, body)
	llmResult := h.llmSafety.ValidateWithContext(ctx, researchMD, issueContext)
	if !llmResult.Passed {
		log.Error("LLM safety check failed for research", "reason", llmResult.Reason)
		return fmt.Errorf("LLM safety check failed: %s", llmResult.Reason)
	}

	// Post on shadow issue with instructions
	commentBody := researchMD + "\n\n---\n\nReply with `approved` to create a PR, `revise` with feedback, or `publish` to post on public issue."
	_, err = h.github.CreateComment(ctx, installationID, shadowRepo, shadowNumber, commentBody)
	if err != nil {
		return fmt.Errorf("post research: %w", err)
	}

	// Store research doc in documents table
	researchEmbedding, err := h.llm.Embed(ctx, fmt.Sprintf("%s\n%s", doc.Title, doc.Summary))
	if err != nil {
		log.Error("embed research doc", "error", err)
	} else {
		if err := h.store.UpsertDocument(ctx, store.Document{
			Repo:      shadowRepo,
			DocType:   "research",
			Title:     doc.Title,
			Content:   researchMD,
			Metadata:  map[string]any{"source_repo": sourceRepo, "issue_number": issueNumber},
			Embedding: researchEmbedding,
		}); err != nil {
			log.Error("store research document", "error", err)
		}
	}

	// Update session to review pending, storing full research markdown
	if err := h.store.UpdateSessionStage(ctx, sessionID, store.StageReviewPending, map[string]any{
		"title": title, "body": body, "research_title": doc.Title, "research_md": researchMD,
	}, currentRoundTrips); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Create audit entry
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sessionID,
		ActionType:        "posted_research",
		InputHash:         hashString(title + body),
		OutputSummary:     truncate(researchMD, 200),
		SafetyCheckPassed: true,
		ConfidenceScore:   llmResult.Confidence,
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	// Create approval gate
	if _, err := h.store.CreateApprovalGate(ctx, store.ApprovalGate{
		SessionID: sessionID,
		GateType:  store.GateResearch,
		Status:    store.ApprovalPending,
	}); err != nil {
		log.Error("create approval gate", "error", err)
	}

	log.Info("posted research document", "docs", len(similarDocs), "issues", len(similarIssues))
	return nil
}

// HandleComment processes a comment on a shadow issue, advancing the agent
// state machine based on the approval signal parsed from the comment.
func (h *AgentHandler) HandleComment(ctx context.Context, installationID int64, shadowRepo string, shadowIssueNumber int, commentBody string, commentUser string) error {
	log := h.logger.With("shadowRepo", shadowRepo, "shadowIssue", shadowIssueNumber, "user", commentUser)

	sess, err := h.store.GetSessionByShadow(ctx, shadowRepo, shadowIssueNumber)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if sess == nil {
		log.Info("no session found for shadow issue")
		return nil
	}
	log = log.With("sessionID", sess.ID, "stage", sess.Stage)

	signal := ParseApprovalSignal(commentBody)
	log.Info("parsed approval signal", "signal", signal)

	// Handle reject signal in any active stage
	if signal == SignalReject {
		return h.handleReject(ctx, installationID, sess, commentBody, commentUser, log)
	}

	var actionErr error
	switch sess.Stage {
	case store.StageNew:
		// Session stuck in "new" — context brief was never posted. Retry.
		log.Info("retrying context brief for stuck session")
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		actionErr = h.retryContextBrief(ctx, installationID, sess, title, body, log)
	case store.StageClarifying:
		actionErr = h.handleClarifyingResponse(ctx, installationID, sess, commentBody, log)
	case store.StageReviewPending:
		actionErr = h.handleReviewResponse(ctx, installationID, sess, signal, commentBody, commentUser, log)
	case store.StageContextBrief:
		actionErr = h.handleContextBriefResponse(ctx, installationID, sess, signal, commentBody, commentUser, log)
	case store.StageApproved:
		if signal == SignalPromote {
			actionErr = h.handlePromote(ctx, installationID, sess, log)
		} else {
			log.Info("session already approved, ignoring non-promote signal")
		}
	default:
		log.Info("ignoring comment in current stage")
	}

	if actionErr != nil {
		errMsg := fmt.Sprintf("Something went wrong processing your `%s` signal. Please try again.\n\n<details><summary>Error details</summary>\n\n```\n%s\n```\n</details>", commentBody, actionErr.Error())
		if _, postErr := h.github.CreateComment(ctx, installationID, shadowRepo, shadowIssueNumber, errMsg); postErr != nil {
			log.Error("failed to post error feedback", "error", postErr)
		}
		return actionErr
	}

	return nil
}

func (h *AgentHandler) handleReject(ctx context.Context, installationID int64, sess *store.AgentSession, commentBody string, commentUser string, log *slog.Logger) error {
	// Resolve any pending gate
	gate, err := h.store.GetPendingGate(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("get pending gate: %w", err)
	}
	if gate != nil {
		_ = h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalRejected, commentUser)
	}

	// Move session to complete
	if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, sess.RoundTripCount); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Post acknowledgment on shadow issue
	ack := "Research session has been rejected and closed."
	_, _ = h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, ack)

	// Create audit entry for rejection
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sess.ID,
		ActionType:        "rejected",
		InputHash:         hashString(commentBody),
		OutputSummary:     "Session rejected by " + commentUser,
		SafetyCheckPassed: true,
		ConfidenceScore:   1.0,
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	log.Info("session rejected")
	return nil
}

func (h *AgentHandler) handleContextBriefResponse(ctx context.Context, installationID int64, sess *store.AgentSession, signal ApprovalSignal, commentBody string, commentUser string, log *slog.Logger) error {
	switch signal {
	case SignalResearch:
		log.Info("research requested, starting full research pipeline")
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, nil)

	case SignalUseAsContext:
		log.Info("context brief acknowledged, closing session")
		if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, sess.RoundTripCount); err != nil {
			return fmt.Errorf("update session stage: %w", err)
		}
		ack := "Context brief acknowledged. Session closed."
		_, _ = h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, ack)
		if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
			SessionID:         sess.ID,
			ActionType:        "context_acknowledged",
			InputHash:         hashString(commentBody),
			OutputSummary:     "Session closed after context brief acknowledged",
			SafetyCheckPassed: true,
			ConfidenceScore:   1.0,
		}); err != nil {
			log.Error("create audit entry", "error", err)
		}
		return nil

	default:
		// Treat any other comment as additional context/corrections and
		// start the research pipeline with the feedback incorporated.
		log.Info("feedback on context brief, starting research with additional context")
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		feedback := []string{commentBody}
		return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, feedback)
	}
}

func (h *AgentHandler) handleClarifyingResponse(ctx context.Context, installationID int64, sess *store.AgentSession, commentBody string, log *slog.Logger) error {
	// Resolve pending gate
	gate, err := h.store.GetPendingGate(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("get pending gate: %w", err)
	}
	if gate != nil {
		if err := h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalApproved, ""); err != nil {
			log.Error("resolve approval gate", "error", err)
		}
	}

	if sess.RoundTripCount >= MaxRoundTrips {
		log.Warn("max round trips reached, escalating")
		escalation := "This enhancement request has reached the maximum number of clarification rounds. A maintainer will need to review it directly."
		_, err := h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, escalation)
		if err != nil {
			log.Error("post escalation", "error", err)
		}
		if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, sess.RoundTripCount); err != nil {
			log.Error("update session stage", "error", err)
		}
		return nil
	}

	// Enrich body with clarification and proceed to research
	title, _ := sess.Context["title"].(string)
	body, _ := sess.Context["body"].(string)
	answers := []string{commentBody}

	return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, answers, sess.RoundTripCount)
}

func (h *AgentHandler) handleReviewResponse(ctx context.Context, installationID int64, sess *store.AgentSession, signal ApprovalSignal, commentBody string, commentUser string, log *slog.Logger) error {
	// Resolve pending gate
	gate, err := h.store.GetPendingGate(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("get pending gate: %w", err)
	}

	switch signal {
	case SignalApproved:
		if gate != nil {
			_ = h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalApproved, commentUser)
		}
		return h.createResearchPR(ctx, installationID, sess, log)

	case SignalRevise:
		if gate != nil {
			_ = h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalRevisionRequested, commentUser)
		}
		newRoundTrips := sess.RoundTripCount + 1
		if newRoundTrips >= MaxRoundTrips {
			log.Warn("max round trips reached on revise, escalating")
			escalation := "This enhancement request has reached the maximum number of revision rounds. A maintainer will need to review it directly."
			_, _ = h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, escalation)
			return h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, newRoundTrips)
		}
		if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageRevision, sess.Context, newRoundTrips); err != nil {
			return fmt.Errorf("update session stage: %w", err)
		}
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		feedback := []string{commentBody}
		return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, feedback, newRoundTrips)

	case SignalPromote:
		if gate != nil {
			_ = h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalApproved, commentUser)
		}
		if err := h.createResearchPR(ctx, installationID, sess, log); err != nil {
			return err
		}
		return h.handlePromote(ctx, installationID, sess, log)

	default:
		// General feedback — resolve pending gate, re-research with additional context
		if gate != nil {
			_ = h.store.ResolveApprovalGate(ctx, gate.ID, store.ApprovalRevisionRequested, commentUser)
		}
		newRoundTrips := sess.RoundTripCount + 1
		if newRoundTrips >= MaxRoundTrips {
			log.Warn("max round trips reached in review, escalating")
			escalation := "This enhancement request has reached the maximum number of revision rounds. A maintainer will need to review it directly."
			_, _ = h.github.CreateComment(ctx, installationID, sess.ShadowRepo, sess.ShadowIssueNumber, escalation)
			return h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, newRoundTrips)
		}
		title, _ := sess.Context["title"].(string)
		body, _ := sess.Context["body"].(string)
		feedback := []string{commentBody}
		return h.startResearch(ctx, installationID, sess.ID, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Repo, sess.IssueNumber, title, body, feedback, newRoundTrips)
	}
}

func (h *AgentHandler) createResearchPR(ctx context.Context, installationID int64, sess *store.AgentSession, log *slog.Logger) error {
	title, _ := sess.Context["title"].(string)
	researchTitle, _ := sess.Context["research_title"].(string)
	if researchTitle == "" {
		researchTitle = title
	}

	slug := slugify(title)
	branchName := "research/" + slug
	date := timeNowDate()
	filePath := fmt.Sprintf("docs/research/%s-%s.md", date, slug)

	// Create branch
	if err := h.github.CreateBranch(ctx, installationID, sess.ShadowRepo, branchName); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	// Use the full research markdown stored during startResearch
	researchContent, _ := sess.Context["research_md"].(string)
	if researchContent == "" {
		// Fallback: generate a minimal document if stored markdown is missing
		body, _ := sess.Context["body"].(string)
		researchContent = FormatResearchMarkdown(&ResearchDocument{
			Title:   researchTitle,
			Summary: truncate(body, 500),
		}, sess.Repo, sess.IssueNumber)
	}

	// Commit research file
	commitMsg := fmt.Sprintf("docs: add research for %s#%d", sess.Repo, sess.IssueNumber)
	if err := h.github.CreateOrUpdateFile(ctx, installationID, sess.ShadowRepo, filePath, branchName, commitMsg, []byte(researchContent)); err != nil {
		return fmt.Errorf("create research file: %w", err)
	}

	// Open PR
	prTitle := fmt.Sprintf("Research: %s", truncate(researchTitle, 60))
	prBody := fmt.Sprintf("Research document for %s#%d\n\nGenerated by the enhancement research agent.", sess.Repo, sess.IssueNumber)
	prNumber, err := h.github.CreatePullRequest(ctx, installationID, sess.ShadowRepo, prTitle, prBody, branchName, "main")
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}

	// Update session
	sessCtx := sess.Context
	if sessCtx == nil {
		sessCtx = map[string]any{}
	}
	sessCtx["pr_number"] = prNumber
	if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageApproved, sessCtx, sess.RoundTripCount); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Create audit entry
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sess.ID,
		ActionType:        "created_pr",
		InputHash:         hashString(title),
		OutputSummary:     fmt.Sprintf("PR #%d created on %s", prNumber, sess.ShadowRepo),
		SafetyCheckPassed: true,
		ConfidenceScore:   1.0,
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	log.Info("created research PR", "pr", prNumber, "branch", branchName)
	return nil
}

func (h *AgentHandler) handlePromote(ctx context.Context, installationID int64, sess *store.AgentSession, log *slog.Logger) error {
	researchTitle, _ := sess.Context["research_title"].(string)
	title, _ := sess.Context["title"].(string)
	if researchTitle == "" {
		researchTitle = title
	}

	// Build a curated summary for the public issue
	summary := fmt.Sprintf("## Enhancement Research: %s\n\nA research document has been prepared for this enhancement request. "+
		"See the shadow repository for full details and discussion.\n\n"+
		"> This summary was generated by the enhancement research agent.",
		researchTitle)

	// Run safety checks before posting publicly
	structResult := h.structural.Validate(summary)
	if !structResult.Passed {
		return fmt.Errorf("structural safety check failed for promotion: %s", structResult.Reason)
	}

	issueContext := fmt.Sprintf("Enhancement: %s", title)
	llmResult := h.llmSafety.ValidateWithContext(ctx, summary, issueContext)
	if !llmResult.Passed {
		return fmt.Errorf("LLM safety check failed for promotion: %s", llmResult.Reason)
	}

	// Post on original public issue
	_, err := h.github.CreateComment(ctx, installationID, sess.Repo, sess.IssueNumber, summary)
	if err != nil {
		return fmt.Errorf("post promotion comment: %w", err)
	}

	// Update session to complete
	if err := h.store.UpdateSessionStage(ctx, sess.ID, store.StageComplete, sess.Context, sess.RoundTripCount); err != nil {
		return fmt.Errorf("update session stage: %w", err)
	}

	// Create audit entry
	if err := h.store.CreateAuditEntry(ctx, store.AuditEntry{
		SessionID:         sess.ID,
		ActionType:        "promoted_to_public",
		InputHash:         hashString(title),
		OutputSummary:     truncate(summary, 200),
		SafetyCheckPassed: true,
		ConfidenceScore:   llmResult.Confidence,
	}); err != nil {
		log.Error("create audit entry", "error", err)
	}

	log.Info("promoted research to public issue")
	return nil
}

// hashString returns the first 8 bytes of the SHA-256 hash of s as hex.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

// truncate returns s shortened to maxLen runes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// slugify converts a string to a URL-friendly slug: lowercase, non-alphanumeric
// characters replaced with dashes, consecutive dashes collapsed, max 50 chars.
func slugify(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			sb.WriteByte('-')
			prevDash = true
		}
	}
	result := strings.Trim(sb.String(), "-")
	if len(result) > 50 {
		result = result[:50]
		result = strings.TrimRight(result, "-")
	}
	return result
}

// timeNowDate returns the current date as "YYYY-MM-DD".
func timeNowDate() string {
	return time.Now().Format("2006-01-02")
}
