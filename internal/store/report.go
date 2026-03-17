package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// DashboardStats holds aggregated statistics for the dashboard.
type DashboardStats struct {
	TotalComments        int             `json:"total_comments"`
	TotalThumbsUp        int             `json:"total_thumbs_up"`
	TotalThumbsDown      int             `json:"total_thumbs_down"`
	PhaseBreakdown       map[string]int  `json:"phase_breakdown"`
	DocumentCounts       map[string]int  `json:"document_counts"`
	IssueCount           int             `json:"issue_count"`
	RecentComments       []RecentComment `json:"recent_comments"`
	TriageStats          *TriageStats    `json:"triage_stats"`
	AgentStats           *AgentStats     `json:"agent_stats"`
	AvgResponseSeconds   *float64        `json:"avg_response_seconds,omitempty"`
	ApprovalGateStats    []GateOutcome   `json:"approval_gate_stats"`
	SafetyStats          *SafetyStats    `json:"safety_stats"`
	RoundTripDistribution []RoundTripBucket `json:"round_trip_distribution"`
	PhaseHitRate          map[string]float64 `json:"phase_hit_rate"`
	FeedbackStats         *FeedbackStats     `json:"feedback_stats"`
	DailyTriageCounts     []DailyBucket      `json:"daily_triage_counts"`
	DailyAgentCounts      []DailyBucket      `json:"daily_agent_counts"`
	DailyFeedbackCounts   []DailyBucket      `json:"daily_feedback_counts"`
}

// TriageStats tracks shadow repo triage outcomes.
type TriageStats struct {
	Total    int              `json:"total"`
	Promoted int              `json:"promoted"`
	Pending  int              `json:"pending"`
	Recent   []RecentTriage   `json:"recent"`
}

// RecentTriage represents a recent triage session for the dashboard.
type RecentTriage struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	ShadowRepo  string `json:"shadow_repo"`
	ShadowIssue int    `json:"shadow_issue"`
	Promoted    bool   `json:"promoted"`
	CreatedAt   string `json:"created_at"`
}

// AgentStats tracks enhancement agent session outcomes.
type AgentStats struct {
	Total          int            `json:"total"`
	StageBreakdown map[string]int `json:"stage_breakdown"`
	ActionBreakdown map[string]int `json:"action_breakdown"`
	Recent         []RecentAgent  `json:"recent"`
}

// RecentAgent represents a recent agent session for the dashboard.
type RecentAgent struct {
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
	ShadowRepo  string `json:"shadow_repo"`
	ShadowIssue int    `json:"shadow_issue"`
	Stage       string `json:"stage"`
	CreatedAt   string `json:"created_at"`
}

// RecentComment represents a recent bot comment for the dashboard.
type RecentComment struct {
	Repo        string   `json:"repo"`
	IssueNumber int      `json:"issue_number"`
	CommentID   int64    `json:"comment_id"`
	PhasesRun   []string `json:"phases_run"`
	ThumbsUp    int      `json:"thumbs_up"`
	ThumbsDown  int      `json:"thumbs_down"`
	CreatedAt   string   `json:"created_at"`
}

// GateOutcome represents an approval gate type/status combination with its count.
type GateOutcome struct {
	GateType string `json:"gate_type"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// SafetyStats holds aggregated safety check statistics.
type SafetyStats struct {
	TotalActions  int      `json:"total_actions"`
	Passed        int      `json:"passed"`
	AvgConfidence *float64 `json:"avg_confidence"`
}

// RoundTripBucket represents a count of sessions with a given round-trip count.
type RoundTripBucket struct {
	RoundTrips int `json:"round_trips"`
	Count      int `json:"count"`
}

// DailyBucket holds a date string and an event count for time-series charts.
type DailyBucket struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GetDashboardStats retrieves aggregated triage statistics for a given repo.
func (s *Store) GetDashboardStats(ctx context.Context, repo string) (*DashboardStats, error) {
	stats := &DashboardStats{
		PhaseBreakdown: make(map[string]int),
		DocumentCounts: make(map[string]int),
		PhaseHitRate:   make(map[string]float64),
		RecentComments: []RecentComment{},
	}

	// Total comments, sum thumbs_up, sum thumbs_down
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(thumbs_up), 0), COALESCE(SUM(thumbs_down), 0)
		FROM bot_comments WHERE repo = $1
	`, repo).Scan(&stats.TotalComments, &stats.TotalThumbsUp, &stats.TotalThumbsDown)
	if err != nil {
		return nil, err
	}

	// Phase breakdown
	rows, err := s.pool.Query(ctx, `
		SELECT phase, count(*) FROM bot_comments, unnest(phases_run) AS phase
		WHERE repo = $1 GROUP BY phase
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var phase string
		var count int
		if err := rows.Scan(&phase, &count); err != nil {
			return nil, err
		}
		stats.PhaseBreakdown[phase] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Document counts by type
	rows2, err := s.pool.Query(ctx, `
		SELECT doc_type, count(*) FROM documents WHERE repo = $1 GROUP BY doc_type
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var docType string
		var count int
		if err := rows2.Scan(&docType, &count); err != nil {
			return nil, err
		}
		stats.DocumentCounts[docType] = count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Issue count
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM issues WHERE repo = $1
	`, repo).Scan(&stats.IssueCount)
	if err != nil {
		return nil, err
	}

	// Recent 25 comments
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
		ORDER BY created_at DESC LIMIT 25
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var rc RecentComment
		var createdAt time.Time
		if err := rows3.Scan(&rc.Repo, &rc.IssueNumber, &rc.CommentID, &rc.PhasesRun, &rc.ThumbsUp, &rc.ThumbsDown, &createdAt); err != nil {
			return nil, err
		}
		rc.CreatedAt = createdAt.Format(time.RFC3339)
		stats.RecentComments = append(stats.RecentComments, rc)
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	// Triage session stats
	triageStats, err := s.getTriageStats(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.TriageStats = triageStats

	// Agent session stats
	agentStats, err := s.getAgentStats(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.AgentStats = agentStats

	// Approval gate outcomes
	gateStats, err := s.getApprovalGateStats(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.ApprovalGateStats = gateStats

	// Safety check stats
	safetyStats, err := s.getSafetyStats(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.SafetyStats = safetyStats

	// Round-trip distribution
	rtDist, err := s.getRoundTripDistribution(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.RoundTripDistribution = rtDist

	// Average time-to-first-response: seconds between issue creation and triage session creation
	var avgSeconds *float64
	err = s.pool.QueryRow(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (t.created_at - i.created_at)))
		FROM triage_sessions t
		INNER JOIN issues i ON t.repo = i.repo AND t.issue_number = i.number
		WHERE t.repo = $1 AND t.created_at > i.created_at
	`, repo).Scan(&avgSeconds)
	if err != nil {
		return nil, err
	}
	stats.AvgResponseSeconds = avgSeconds

	// Phase hit rate
	phaseHitRate, err := s.getPhaseHitRate(ctx, repo)
	if err != nil {
		return nil, err
	}
	stats.PhaseHitRate = phaseHitRate

	// Feedback stats (non-fatal only if table doesn't exist yet — migration 010)
	feedbackStats, err := s.GetFeedbackStats(ctx, repo)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			slog.Warn("feedback_signals table not found, skipping", "error", err)
		} else {
			return nil, err
		}
	} else {
		stats.FeedbackStats = feedbackStats
	}

	// Daily time-series counts (non-fatal)
	if dailyTriage, err := s.GetDailyTriageCounts(ctx, repo); err != nil {
		slog.Warn("failed to get daily triage counts", "error", err)
	} else {
		stats.DailyTriageCounts = dailyTriage
	}

	if dailyAgent, err := s.GetDailyAgentCounts(ctx, repo); err != nil {
		slog.Warn("failed to get daily agent counts", "error", err)
	} else {
		stats.DailyAgentCounts = dailyAgent
	}

	if dailyFeedback, err := s.GetDailyFeedbackCounts(ctx, repo); err != nil {
		slog.Warn("failed to get daily feedback counts", "error", err)
	} else {
		stats.DailyFeedbackCounts = dailyFeedback
	}

	return stats, nil
}

func (s *Store) getTriageStats(ctx context.Context, repo string) (*TriageStats, error) {
	ts := &TriageStats{Recent: []RecentTriage{}}

	// Total triage sessions
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM triage_sessions WHERE repo = $1
	`, repo).Scan(&ts.Total)
	if err != nil {
		return nil, err
	}

	// Promoted = triage sessions that have a matching bot_comment (lgtm was used)
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM triage_sessions t
		INNER JOIN bot_comments b ON t.repo = b.repo AND t.issue_number = b.issue_number
		WHERE t.repo = $1
	`, repo).Scan(&ts.Promoted)
	if err != nil {
		return nil, err
	}

	ts.Pending = ts.Total - ts.Promoted

	// Recent 25 triage sessions
	rows, err := s.pool.Query(ctx, `
		SELECT t.repo, t.issue_number, t.shadow_repo, t.shadow_issue_number,
			EXISTS(SELECT 1 FROM bot_comments b WHERE b.repo = t.repo AND b.issue_number = t.issue_number) AS promoted,
			t.created_at
		FROM triage_sessions t WHERE t.repo = $1
		ORDER BY t.created_at DESC LIMIT 25
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rt RecentTriage
		var createdAt time.Time
		if err := rows.Scan(&rt.Repo, &rt.IssueNumber, &rt.ShadowRepo, &rt.ShadowIssue, &rt.Promoted, &createdAt); err != nil {
			return nil, err
		}
		rt.CreatedAt = createdAt.Format(time.RFC3339)
		ts.Recent = append(ts.Recent, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ts, nil
}

func (s *Store) getAgentStats(ctx context.Context, repo string) (*AgentStats, error) {
	as := &AgentStats{
		StageBreakdown:  make(map[string]int),
		ActionBreakdown: make(map[string]int),
		Recent:          []RecentAgent{},
	}

	// Total agent sessions
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM agent_sessions WHERE repo = $1
	`, repo).Scan(&as.Total)
	if err != nil {
		return nil, err
	}

	// Stage breakdown
	rows, err := s.pool.Query(ctx, `
		SELECT stage, COUNT(*) FROM agent_sessions WHERE repo = $1 GROUP BY stage
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stage string
		var count int
		if err := rows.Scan(&stage, &count); err != nil {
			return nil, err
		}
		as.StageBreakdown[stage] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Action breakdown from audit log
	rows2, err := s.pool.Query(ctx, `
		SELECT a.action_type, COUNT(*) FROM agent_audit_log a
		INNER JOIN agent_sessions s ON a.session_id = s.id
		WHERE s.repo = $1 GROUP BY a.action_type
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var action string
		var count int
		if err := rows2.Scan(&action, &count); err != nil {
			return nil, err
		}
		as.ActionBreakdown[action] = count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Recent 25 agent sessions
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, shadow_repo, shadow_issue_number, stage, created_at
		FROM agent_sessions WHERE repo = $1
		ORDER BY created_at DESC LIMIT 25
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var ra RecentAgent
		var createdAt time.Time
		if err := rows3.Scan(&ra.Repo, &ra.IssueNumber, &ra.ShadowRepo, &ra.ShadowIssue, &ra.Stage, &createdAt); err != nil {
			return nil, err
		}
		ra.CreatedAt = createdAt.Format(time.RFC3339)
		as.Recent = append(as.Recent, ra)
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	return as, nil
}

func (s *Store) getApprovalGateStats(ctx context.Context, repo string) ([]GateOutcome, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ag.gate_type, ag.status, COUNT(*) AS count
		FROM approval_gates ag
		INNER JOIN agent_sessions s ON ag.session_id = s.id
		WHERE s.repo = $1
		GROUP BY ag.gate_type, ag.status
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []GateOutcome
	for rows.Next() {
		var g GateOutcome
		if err := rows.Scan(&g.GateType, &g.Status, &g.Count); err != nil {
			return nil, err
		}
		results = append(results, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []GateOutcome{}
	}
	return results, nil
}

func (s *Store) getSafetyStats(ctx context.Context, repo string) (*SafetyStats, error) {
	ss := &SafetyStats{}
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total_actions,
			COUNT(CASE WHEN al.safety_check_passed THEN 1 END) AS passed,
			AVG(al.confidence_score) AS avg_confidence
		FROM agent_audit_log al
		INNER JOIN agent_sessions s ON al.session_id = s.id
		WHERE s.repo = $1
	`, repo).Scan(&ss.TotalActions, &ss.Passed, &ss.AvgConfidence)
	if err != nil {
		return nil, err
	}
	return ss, nil
}

func (s *Store) getRoundTripDistribution(ctx context.Context, repo string) ([]RoundTripBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT round_trip_count, COUNT(*) AS count
		FROM agent_sessions
		WHERE repo = $1
		GROUP BY round_trip_count
		ORDER BY round_trip_count
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RoundTripBucket
	for rows.Next() {
		var b RoundTripBucket
		if err := rows.Scan(&b.RoundTrips, &b.Count); err != nil {
			return nil, err
		}
		results = append(results, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []RoundTripBucket{}
	}
	return results, nil
}

func (s *Store) getPhaseHitRate(ctx context.Context, repo string) (map[string]float64, error) {
	result := map[string]float64{}
	rows, err := s.pool.Query(ctx, `
		SELECT phase,
			COUNT(DISTINCT t.id)::float / NULLIF(
				(SELECT COUNT(*) FROM triage_sessions WHERE repo = $1), 0
			) AS hit_rate
		FROM triage_sessions t, unnest(t.phases_run) AS phase
		WHERE t.repo = $1
		GROUP BY phase
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var phase string
		var rate float64
		if err := rows.Scan(&phase, &rate); err != nil {
			return nil, err
		}
		result[phase] = rate
	}
	return result, rows.Err()
}

// UpdateReactions updates the thumbs up/down counts for a bot comment.
func (s *Store) UpdateReactions(ctx context.Context, repo string, issueNumber, thumbsUp, thumbsDown int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE bot_comments SET thumbs_up = $3, thumbs_down = $4
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber, thumbsUp, thumbsDown)
	return err
}

// GetDailyTriageCounts returns a 30-day time series of triage session counts.
func (s *Store) GetDailyTriageCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date AS date, COALESCE(COUNT(t.id), 0) AS count
		FROM generate_series(
			NOW() - INTERVAL '29 days',
			NOW(),
			INTERVAL '1 day'
		) AS d
		LEFT JOIN triage_sessions t
			ON t.repo = $1 AND t.created_at::date = d::date
		GROUP BY d::date
		ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDailyBuckets(rows)
}

// GetDailyAgentCounts returns a 30-day time series of agent session counts.
func (s *Store) GetDailyAgentCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date AS date, COALESCE(COUNT(a.id), 0) AS count
		FROM generate_series(
			NOW() - INTERVAL '29 days',
			NOW(),
			INTERVAL '1 day'
		) AS d
		LEFT JOIN agent_sessions a
			ON a.repo = $1 AND a.created_at::date = d::date
		GROUP BY d::date
		ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDailyBuckets(rows)
}

// GetDailyFeedbackCounts returns a 30-day time series of feedback signal counts.
func (s *Store) GetDailyFeedbackCounts(ctx context.Context, repo string) ([]DailyBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d::date AS date, COALESCE(COUNT(f.id), 0) AS count
		FROM generate_series(
			NOW() - INTERVAL '29 days',
			NOW(),
			INTERVAL '1 day'
		) AS d
		LEFT JOIN feedback_signals f
			ON f.repo = $1 AND f.created_at::date = d::date
		GROUP BY d::date
		ORDER BY d::date
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDailyBuckets(rows)
}

// scanDailyBuckets reads rows of (date, count) into a []DailyBucket slice.
func scanDailyBuckets(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]DailyBucket, error) {
	var buckets []DailyBucket
	for rows.Next() {
		var b DailyBucket
		var d time.Time
		if err := rows.Scan(&d, &b.Count); err != nil {
			return nil, err
		}
		b.Date = d.Format("2006-01-02")
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if buckets == nil {
		buckets = []DailyBucket{}
	}
	return buckets, nil
}

// TriageDetail holds full detail for a single triage session.
type TriageDetail struct {
	ID            int64    `json:"id"`
	Repo          string   `json:"repo"`
	IssueNumber   int      `json:"issue_number"`
	ShadowRepo    string   `json:"shadow_repo"`
	ShadowIssue   int      `json:"shadow_issue"`
	TriageComment string   `json:"triage_comment"`
	PhasesRun     []string `json:"phases_run"`
	Promoted      bool     `json:"promoted"`
	CreatedAt     string   `json:"created_at"`
}

// AuditLogEntry records a single action taken by the agent, for dashboard drill-down.
// Named AuditLogEntry to avoid collision with the AuditEntry type in models.go.
type AuditLogEntry struct {
	ActionType      string  `json:"action_type"`
	OutputSummary   string  `json:"output_summary"`
	SafetyPassed    bool    `json:"safety_passed"`
	ConfidenceScore float64 `json:"confidence_score"`
	CreatedAt       string  `json:"created_at"`
}

// AgentDetail holds full detail for a single agent session including its audit log.
type AgentDetail struct {
	ID             int64           `json:"id"`
	Repo           string          `json:"repo"`
	IssueNumber    int             `json:"issue_number"`
	ShadowRepo     string          `json:"shadow_repo"`
	ShadowIssue    int             `json:"shadow_issue"`
	Stage          string          `json:"stage"`
	RoundTripCount int             `json:"round_trip_count"`
	AuditLog       []AuditLogEntry `json:"audit_log"`
	CreatedAt      string          `json:"created_at"`
}

// GetTriageSessionDetail returns full detail for the triage session matching repo+issueNumber.
// It returns nil if no session is found.
func (s *Store) GetTriageSessionDetail(ctx context.Context, repo string, issueNumber int) (*TriageDetail, error) {
	d := &TriageDetail{}
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.repo, t.issue_number, t.shadow_repo, t.shadow_issue_number,
			t.triage_comment, t.phases_run,
			EXISTS(SELECT 1 FROM bot_comments b WHERE b.repo = t.repo AND b.issue_number = t.issue_number) AS promoted,
			t.created_at
		FROM triage_sessions t
		WHERE t.repo = $1 AND t.issue_number = $2
	`, repo, issueNumber).Scan(
		&d.ID, &d.Repo, &d.IssueNumber, &d.ShadowRepo, &d.ShadowIssue,
		&d.TriageComment, &d.PhasesRun, &d.Promoted, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Format(time.RFC3339)
	return d, nil
}

// GetAgentSessionDetail returns full detail for the agent session matching repo+issueNumber,
// including all audit log entries. It returns nil if no session is found.
func (s *Store) GetAgentSessionDetail(ctx context.Context, repo string, issueNumber int) (*AgentDetail, error) {
	d := &AgentDetail{}
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, repo, issue_number, shadow_repo, shadow_issue_number,
			stage, round_trip_count, created_at
		FROM agent_sessions
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber).Scan(
		&d.ID, &d.Repo, &d.IssueNumber, &d.ShadowRepo, &d.ShadowIssue,
		&d.Stage, &d.RoundTripCount, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Format(time.RFC3339)

	rows, err := s.pool.Query(ctx, `
		SELECT action_type, output_summary, safety_check_passed, confidence_score, created_at
		FROM agent_audit_log
		WHERE session_id = $1
		ORDER BY created_at ASC
	`, d.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d.AuditLog = []AuditLogEntry{}
	for rows.Next() {
		var entry AuditLogEntry
		var entryCreatedAt time.Time
		if err := rows.Scan(&entry.ActionType, &entry.OutputSummary, &entry.SafetyPassed, &entry.ConfidenceScore, &entryCreatedAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = entryCreatedAt.Format(time.RFC3339)
		d.AuditLog = append(d.AuditLog, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return d, nil
}

// ListBotComments returns all bot comments for a given repo.
func (s *Store) ListBotComments(ctx context.Context, repo string) ([]BotComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []BotComment
	for rows.Next() {
		var bc BotComment
		if err := rows.Scan(&bc.ID, &bc.Repo, &bc.IssueNumber, &bc.CommentID, &bc.PhasesRun, &bc.ThumbsUp, &bc.ThumbsDown, &bc.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, bc)
	}
	return comments, rows.Err()
}
