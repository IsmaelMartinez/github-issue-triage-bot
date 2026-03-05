package store

import "time"

// Document represents a documentation chunk (troubleshooting, roadmap, ADR, research).
type Document struct {
	ID        int64
	Repo      string
	DocType   string
	Title     string
	Content   string
	Metadata  map[string]any
	Embedding []float32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Issue represents a GitHub issue with its embedding.
type Issue struct {
	ID        int64
	Repo      string
	Number    int
	Title     string
	Summary   string
	State     string
	Labels    []string
	Milestone *string
	Embedding []float32
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  *time.Time
}

// BotComment tracks a bot comment on a GitHub issue.
type BotComment struct {
	ID          int64
	Repo        string
	IssueNumber int
	CommentID   int64
	PhasesRun   []string
	ThumbsUp    int
	ThumbsDown  int
	CreatedAt   time.Time
}

// SimilarDocument is returned from similarity search with a distance score.
type SimilarDocument struct {
	Document
	Distance float64
}

// SimilarIssue is returned from similarity search with a distance score.
type SimilarIssue struct {
	Issue
	Distance float64
}

// EnhancementDocTypes lists the document types that Phase 4a searches for
// enhancement context. The seed command validates against this list.
var EnhancementDocTypes = []string{"roadmap", "adr", "research"}

// Agent session stage constants.
const (
	StageNew           = "new"
	StageClarifying    = "clarifying"
	StageResearching   = "researching"
	StageReviewPending = "review_pending"
	StageRevision      = "revision"
	StageApproved      = "approved"
	StageComplete      = "complete"
	StageContextBrief  = "context_brief"
)

// Approval gate type constants.
const (
	GateClarification  = "clarification_approval"
	GateResearch       = "research_approval"
	GatePR             = "pr_approval"
	GatePromotePublic  = "promote_to_public"
)

// Approval status constants.
const (
	ApprovalPending           = "pending"
	ApprovalApproved          = "approved"
	ApprovalRejected          = "rejected"
	ApprovalRevisionRequested = "revision_requested"
)

// ApprovalMode controls how the agent handles the review gate.
const (
	ApprovalModeManual     = "manual"     // always require human approval (default)
	ApprovalModeConfidence = "confidence" // auto-approve if quality score >= threshold
)

// AgentSession tracks an agentic issue-resolution session.
type AgentSession struct {
	ID                 int64
	Repo               string
	IssueNumber        int
	ShadowRepo         string
	ShadowIssueNumber  int
	Stage              string
	Context            map[string]any
	RoundTripCount     int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AuditEntry records a single action taken by the agent for auditability.
type AuditEntry struct {
	ID                int64
	SessionID         int64
	ActionType        string
	InputHash         string
	OutputSummary     string
	SafetyCheckPassed bool
	ConfidenceScore   float32
	QualityScore      *int // holdout judge score (0-100), nil if not evaluated
	CreatedAt         time.Time
}

// TriageSession tracks a shadow issue created for triage review.
type TriageSession struct {
	ID                int64
	Repo              string
	IssueNumber       int
	ShadowRepo        string
	ShadowIssueNumber int
	TriageComment     string
	PhasesRun         []string
	CreatedAt         time.Time
}

// ApprovalGate represents a human-in-the-loop checkpoint in the agent workflow.
type ApprovalGate struct {
	ID         int64
	SessionID  int64
	GateType   string
	Status     string
	Approver   string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}
