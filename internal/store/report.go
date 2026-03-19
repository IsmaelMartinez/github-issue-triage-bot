package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
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

// ClampWeeks normalises a week count to [1, 52], defaulting 0 or negative to 12.
func ClampWeeks(w int) int {
	if w <= 0 {
		return 12
	}
	if w > 52 {
		return 52
	}
	return w
}

// WeeklyTrends holds multi-metric weekly time-series data for trend reports.
type WeeklyTrends struct {
	Repo      string            `json:"repo"`
	Weeks     int               `json:"weeks"`
	Triage    []WeeklyTriage    `json:"triage"`
	Phases    []WeeklyPhases    `json:"phases"`
	Response  []WeeklyResponse  `json:"response_time"`
	Agents    []WeeklyAgents    `json:"agents"`
	Synthesis []WeeklySynthesis `json:"synthesis"`
	Feedback  []WeeklyFeedback  `json:"feedback"`
}

// WeeklyTriage tracks triage volume and promotion rate per week.
type WeeklyTriage struct {
	Week     string  `json:"week"`
	Total    int     `json:"total"`
	Promoted int     `json:"promoted"`
	Rate     float64 `json:"rate"`
}

// WeeklyPhases tracks phase hit rates per week.
type WeeklyPhases struct {
	Week    string  `json:"week"`
	Phase1  float64 `json:"phase1"`
	Phase2  float64 `json:"phase2"`
	Phase4a float64 `json:"phase4a"`
}

// WeeklyResponse tracks average response time per week.
type WeeklyResponse struct {
	Week       string  `json:"week"`
	AvgSeconds float64 `json:"avg_seconds"`
}

// WeeklyAgents tracks agent session outcomes per week.
type WeeklyAgents struct {
	Week     string `json:"week"`
	Total    int    `json:"total"`
	Approved int    `json:"approved"`
	Rejected int    `json:"rejected"`
	Pending  int    `json:"pending"`
	Complete int    `json:"complete"`
}

// WeeklySynthesis tracks synthesis output per week.
type WeeklySynthesis struct {
	Week      string `json:"week"`
	Briefings int    `json:"briefings"`
	Findings  int    `json:"findings"`
}

// WeeklyFeedback tracks feedback signals per week.
type WeeklyFeedback struct {
	Week      string `json:"week"`
	EditFills int    `json:"edit_fills"`
	Mentions  int    `json:"mentions"`
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
		SELECT d::date, COALESCE(t.cnt, 0)
		FROM generate_series(CURRENT_DATE - INTERVAL '29 days', CURRENT_DATE, INTERVAL '1 day') AS d
		LEFT JOIN (
			SELECT created_at::date AS day, COUNT(*) AS cnt
			FROM triage_sessions
			WHERE repo = $1 AND created_at >= CURRENT_DATE - INTERVAL '29 days' AND created_at < CURRENT_DATE + INTERVAL '1 day'
			GROUP BY created_at::date
		) t ON t.day = d::date
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
		SELECT d::date, COALESCE(a.cnt, 0)
		FROM generate_series(CURRENT_DATE - INTERVAL '29 days', CURRENT_DATE, INTERVAL '1 day') AS d
		LEFT JOIN (
			SELECT created_at::date AS day, COUNT(*) AS cnt
			FROM agent_sessions
			WHERE repo = $1 AND created_at >= CURRENT_DATE - INTERVAL '29 days' AND created_at < CURRENT_DATE + INTERVAL '1 day'
			GROUP BY created_at::date
		) a ON a.day = d::date
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
		SELECT d::date, COALESCE(f.cnt, 0)
		FROM generate_series(CURRENT_DATE - INTERVAL '29 days', CURRENT_DATE, INTERVAL '1 day') AS d
		LEFT JOIN (
			SELECT created_at::date AS day, COUNT(*) AS cnt
			FROM feedback_signals
			WHERE repo = $1 AND created_at >= CURRENT_DATE - INTERVAL '29 days' AND created_at < CURRENT_DATE + INTERVAL '1 day'
			GROUP BY created_at::date
		) f ON f.day = d::date
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

// GetWeeklyTrends returns multi-metric weekly time-series data for the given repo.
// Each query is independent; partial results are returned if individual queries fail.
func (s *Store) GetWeeklyTrends(ctx context.Context, repo string, weeks int) (*WeeklyTrends, error) {
	w := ClampWeeks(weeks)
	cutoff := time.Now().Add(-time.Duration(w) * 7 * 24 * time.Hour)

	result := &WeeklyTrends{
		Repo:      repo,
		Weeks:     w,
		Triage:    []WeeklyTriage{},
		Phases:    []WeeklyPhases{},
		Response:  []WeeklyResponse{},
		Agents:    []WeeklyAgents{},
		Synthesis: []WeeklySynthesis{},
		Feedback:  []WeeklyFeedback{},
	}

	log := slog.Default()
	var errs []error

	// Query 1: Triage volume + promotion rate
	rows, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(t.total, 0), COALESCE(t.promoted, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', ts.created_at) AS week,
				COUNT(*) AS total,
				COUNT(bc.issue_number) AS promoted
			FROM triage_sessions ts
			LEFT JOIN bot_comments bc ON ts.repo = bc.repo AND ts.issue_number = bc.issue_number
			WHERE ts.repo = $2 AND ts.created_at >= $1::timestamptz
			GROUP BY date_trunc('week', ts.created_at)
		) t ON t.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: triage query failed", "error", err)
		errs = append(errs, fmt.Errorf("triage query: %w", err))
	} else {
		defer rows.Close()
		for rows.Next() {
			var wt WeeklyTriage
			var d time.Time
			if err := rows.Scan(&d, &wt.Total, &wt.Promoted); err != nil {
				log.Warn("weekly trends: triage scan failed", "error", err)
				errs = append(errs, fmt.Errorf("triage scan: %w", err))
				break
			}
			wt.Week = d.Format("2006-01-02")
			if wt.Total > 0 {
				wt.Rate = float64(wt.Promoted) / float64(wt.Total)
			}
			result.Triage = append(result.Triage, wt)
		}
		if err := rows.Err(); err != nil {
			errs = append(errs, fmt.Errorf("triage rows: %w", err))
		}
	}

	// Query 2: Phase hit rates
	rows2, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(p.phase1, 0), COALESCE(p.phase2, 0), COALESCE(p.phase4a, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', ts.created_at) AS week,
				COUNT(CASE WHEN 'phase1' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase1,
				COUNT(CASE WHEN 'phase2' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase2,
				COUNT(CASE WHEN 'phase4a' = ANY(ts.phases_run) THEN 1 END)::float / NULLIF(COUNT(*), 0) AS phase4a
			FROM triage_sessions ts
			WHERE ts.repo = $2 AND ts.created_at >= $1::timestamptz
			GROUP BY date_trunc('week', ts.created_at)
		) p ON p.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: phases query failed", "error", err)
		errs = append(errs, fmt.Errorf("phases query: %w", err))
	} else {
		defer rows2.Close()
		for rows2.Next() {
			var wp WeeklyPhases
			var d time.Time
			if err := rows2.Scan(&d, &wp.Phase1, &wp.Phase2, &wp.Phase4a); err != nil {
				log.Warn("weekly trends: phases scan failed", "error", err)
				errs = append(errs, fmt.Errorf("phases scan: %w", err))
				break
			}
			wp.Week = d.Format("2006-01-02")
			result.Phases = append(result.Phases, wp)
		}
		if err := rows2.Err(); err != nil {
			errs = append(errs, fmt.Errorf("phases rows: %w", err))
		}
	}

	// Query 3: Average response time
	rows3, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(r.avg_secs, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', t.created_at) AS week,
				AVG(EXTRACT(EPOCH FROM (t.created_at - i.created_at))) AS avg_secs
			FROM triage_sessions t
			INNER JOIN issues i ON t.repo = i.repo AND t.issue_number = i.number
			WHERE t.repo = $2 AND t.created_at >= $1::timestamptz AND t.created_at > i.created_at
			GROUP BY date_trunc('week', t.created_at)
		) r ON r.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: response query failed", "error", err)
		errs = append(errs, fmt.Errorf("response query: %w", err))
	} else {
		defer rows3.Close()
		for rows3.Next() {
			var wr WeeklyResponse
			var d time.Time
			if err := rows3.Scan(&d, &wr.AvgSeconds); err != nil {
				log.Warn("weekly trends: response scan failed", "error", err)
				errs = append(errs, fmt.Errorf("response scan: %w", err))
				break
			}
			wr.Week = d.Format("2006-01-02")
			result.Response = append(result.Response, wr)
		}
		if err := rows3.Err(); err != nil {
			errs = append(errs, fmt.Errorf("response rows: %w", err))
		}
	}

	// Query 4: Agent sessions (stage breakdown)
	rows4, err := s.pool.Query(ctx, `
		SELECT w.week::date,
			COALESCE(a.total, 0), COALESCE(a.approved, 0), COALESCE(a.rejected, 0),
			COALESCE(a.pending, 0), COALESCE(a.complete, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(*) AS total,
				COUNT(CASE WHEN stage = 'approved' THEN 1 END) AS approved,
				COUNT(CASE WHEN stage = 'revision' THEN 1 END) AS rejected,
				COUNT(CASE WHEN stage IN ('new', 'clarifying', 'researching', 'review_pending', 'context_brief') THEN 1 END) AS pending,
				COUNT(CASE WHEN stage = 'complete' THEN 1 END) AS complete
			FROM agent_sessions
			WHERE repo = $2 AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) a ON a.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: agents query failed", "error", err)
		errs = append(errs, fmt.Errorf("agents query: %w", err))
	} else {
		defer rows4.Close()
		for rows4.Next() {
			var wa WeeklyAgents
			var d time.Time
			if err := rows4.Scan(&d, &wa.Total, &wa.Approved, &wa.Rejected, &wa.Pending, &wa.Complete); err != nil {
				log.Warn("weekly trends: agents scan failed", "error", err)
				errs = append(errs, fmt.Errorf("agents scan: %w", err))
				break
			}
			wa.Week = d.Format("2006-01-02")
			result.Agents = append(result.Agents, wa)
		}
		if err := rows4.Err(); err != nil {
			errs = append(errs, fmt.Errorf("agents rows: %w", err))
		}
	}

	// Query 5: Synthesis briefings (from repo_events)
	rows5, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(b.briefings, 0), COALESCE(b.findings, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(*) AS briefings,
				COALESCE(SUM(CASE WHEN metadata->>'findings' ~ '^\d+$' THEN (metadata->>'findings')::int ELSE 0 END), 0) AS findings
			FROM repo_events
			WHERE repo = $2 AND event_type = 'briefing_posted' AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) b ON b.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: synthesis query failed", "error", err)
		errs = append(errs, fmt.Errorf("synthesis query: %w", err))
	} else {
		defer rows5.Close()
		for rows5.Next() {
			var ws WeeklySynthesis
			var d time.Time
			if err := rows5.Scan(&d, &ws.Briefings, &ws.Findings); err != nil {
				log.Warn("weekly trends: synthesis scan failed", "error", err)
				errs = append(errs, fmt.Errorf("synthesis scan: %w", err))
				break
			}
			ws.Week = d.Format("2006-01-02")
			result.Synthesis = append(result.Synthesis, ws)
		}
		if err := rows5.Err(); err != nil {
			errs = append(errs, fmt.Errorf("synthesis rows: %w", err))
		}
	}

	// Query 6: Feedback signals
	rows6, err := s.pool.Query(ctx, `
		SELECT w.week::date, COALESCE(f.edit_fills, 0), COALESCE(f.mentions, 0)
		FROM generate_series(
			date_trunc('week', $1::timestamptz),
			date_trunc('week', NOW()),
			'1 week'::interval
		) AS w(week)
		LEFT JOIN (
			SELECT date_trunc('week', created_at) AS week,
				COUNT(CASE WHEN signal_type = 'issue_edit_filled' THEN 1 END) AS edit_fills,
				COUNT(CASE WHEN signal_type = 'user_mention' THEN 1 END) AS mentions
			FROM feedback_signals
			WHERE repo = $2 AND created_at >= $1::timestamptz
			GROUP BY date_trunc('week', created_at)
		) f ON f.week = w.week
		ORDER BY w.week
	`, cutoff, repo)
	if err != nil {
		log.Warn("weekly trends: feedback query failed", "error", err)
		errs = append(errs, fmt.Errorf("feedback query: %w", err))
	} else {
		defer rows6.Close()
		for rows6.Next() {
			var wf WeeklyFeedback
			var d time.Time
			if err := rows6.Scan(&d, &wf.EditFills, &wf.Mentions); err != nil {
				log.Warn("weekly trends: feedback scan failed", "error", err)
				errs = append(errs, fmt.Errorf("feedback scan: %w", err))
				break
			}
			wf.Week = d.Format("2006-01-02")
			result.Feedback = append(result.Feedback, wf)
		}
		if err := rows6.Err(); err != nil {
			errs = append(errs, fmt.Errorf("feedback rows: %w", err))
		}
	}

	return result, errors.Join(errs...)
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
		if err == pgx.ErrNoRows {
			return nil, nil
		}
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
		if err == pgx.ErrNoRows {
			return nil, nil
		}
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
