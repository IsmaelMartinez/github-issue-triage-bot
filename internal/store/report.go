package store

import (
	"context"
	"time"
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

// GetDashboardStats retrieves aggregated triage statistics for a given repo.
func (s *Store) GetDashboardStats(ctx context.Context, repo string) (*DashboardStats, error) {
	stats := &DashboardStats{
		PhaseBreakdown: make(map[string]int),
		DocumentCounts: make(map[string]int),
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

	// Recent 20 comments
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, comment_id, phases_run, thumbs_up, thumbs_down, created_at
		FROM bot_comments WHERE repo = $1
		ORDER BY created_at DESC LIMIT 20
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

	// Recent 10 triage sessions
	rows, err := s.pool.Query(ctx, `
		SELECT t.repo, t.issue_number, t.shadow_repo, t.shadow_issue_number,
			EXISTS(SELECT 1 FROM bot_comments b WHERE b.repo = t.repo AND b.issue_number = t.issue_number) AS promoted,
			t.created_at
		FROM triage_sessions t WHERE t.repo = $1
		ORDER BY t.created_at DESC LIMIT 10
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

	// Recent 10 agent sessions
	rows3, err := s.pool.Query(ctx, `
		SELECT repo, issue_number, shadow_repo, shadow_issue_number, stage, created_at
		FROM agent_sessions WHERE repo = $1
		ORDER BY created_at DESC LIMIT 10
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

// UpdateReactions updates the thumbs up/down counts for a bot comment.
func (s *Store) UpdateReactions(ctx context.Context, repo string, issueNumber, thumbsUp, thumbsDown int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE bot_comments SET thumbs_up = $3, thumbs_down = $4
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber, thumbsUp, thumbsDown)
	return err
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
