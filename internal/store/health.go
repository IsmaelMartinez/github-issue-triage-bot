package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// HealthMetrics holds operational health metrics queried from existing tables.
type HealthMetrics struct {
	ConfidenceRecent7d  *float64 `json:"confidence_recent_7d"`
	ConfidenceAllTime   *float64 `json:"confidence_all_time"`
	StuckSessionCount   int      `json:"stuck_session_count"`
	TotalRecentSessions int      `json:"total_recent_sessions"`
	OrphanedTriageCount int      `json:"orphaned_triage_count"`
	TotalTriageSessions int      `json:"total_triage_sessions"`
	CheckedAt           string   `json:"checked_at"`
}

// HealthAlert represents a single threshold violation detected by the health monitor.
type HealthAlert struct {
	Metric    string  `json:"metric"`
	Current   float64 `json:"current"`
	Threshold float64 `json:"threshold"`
	Message   string  `json:"message"`
}

// GetHealthMetrics queries operational health metrics from existing tables.
// Each query is independent; partial results are returned if individual queries fail.
func (s *Store) GetHealthMetrics(ctx context.Context, repo string) (*HealthMetrics, error) {
	m := &HealthMetrics{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	log := slog.Default()
	var errs []error

	// Confidence scores: recent 7-day average and all-time average
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT AVG(al.confidence_score) FROM agent_audit_log al
			 INNER JOIN agent_sessions s ON al.session_id = s.id
			 WHERE s.repo = $1 AND al.created_at > now() - interval '7 days'),
			(SELECT AVG(al.confidence_score) FROM agent_audit_log al
			 INNER JOIN agent_sessions s ON al.session_id = s.id
			 WHERE s.repo = $1)
	`, repo).Scan(&m.ConfidenceRecent7d, &m.ConfidenceAllTime)
	if err != nil {
		log.Warn("health check: confidence score query failed", "error", err)
		errs = append(errs, fmt.Errorf("confidence score query: %w", err))
	}

	// Stuck sessions: only count sessions in 'new' stage (should auto-advance immediately).
	// Sessions in context_brief, review_pending, researching, revision are legitimately
	// waiting for maintainer review in shadow repos.
	err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM agent_sessions
			 WHERE repo = $1 AND stage = 'new' AND updated_at < now() - interval '1 hour'),
			(SELECT COUNT(*) FROM agent_sessions
			 WHERE repo = $1 AND created_at > now() - interval '30 days')
	`, repo).Scan(&m.StuckSessionCount, &m.TotalRecentSessions)
	if err != nil {
		log.Warn("health check: stuck sessions query failed", "error", err)
		errs = append(errs, fmt.Errorf("stuck sessions query: %w", err))
	}

	// Orphaned triage sessions: no bot_comment, not closed, older than 14 days.
	// Shadow triage sessions legitimately wait for maintainer review (lgtm/reject),
	// so 14 days matches the stale cleanup threshold.
	err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM triage_sessions t
			 LEFT JOIN bot_comments b ON t.repo = b.repo AND t.issue_number = b.issue_number
			 WHERE t.repo = $1 AND b.id IS NULL AND t.closed_at IS NULL AND t.created_at < now() - interval '14 days'),
			(SELECT COUNT(*) FROM triage_sessions
			 WHERE repo = $1 AND created_at > now() - interval '30 days')
	`, repo).Scan(&m.OrphanedTriageCount, &m.TotalTriageSessions)
	if err != nil {
		log.Warn("health check: orphaned triage query failed", "error", err)
		errs = append(errs, fmt.Errorf("orphaned triage query: %w", err))
	}

	return m, errors.Join(errs...)
}

// EvaluateThresholds checks health metrics against degradation thresholds
// and returns any alerts. This is a pure function with no side effects.
func EvaluateThresholds(m *HealthMetrics) []HealthAlert {
	var alerts []HealthAlert

	// Confidence degradation: recent 7d average < 80% of all-time average
	if m.ConfidenceRecent7d != nil && m.ConfidenceAllTime != nil && *m.ConfidenceAllTime > 0 {
		threshold := *m.ConfidenceAllTime * 0.8
		if *m.ConfidenceRecent7d < threshold {
			alerts = append(alerts, HealthAlert{
				Metric:    "confidence_degradation",
				Current:   *m.ConfidenceRecent7d,
				Threshold: threshold,
				Message:   "7-day average confidence score has dropped below 80% of the all-time average",
			})
		}
	}

	// Stuck sessions: more than 2 sessions stuck for > 1 hour
	if m.StuckSessionCount > 2 {
		alerts = append(alerts, HealthAlert{
			Metric:    "stuck_sessions",
			Current:   float64(m.StuckSessionCount),
			Threshold: 2,
			Message:   "More than 2 agent sessions are stuck in non-terminal stages for over 1 hour",
		})
	}

	// Orphaned triage: more than 3 triage sessions without bot_comment or closure
	if m.OrphanedTriageCount > 3 {
		alerts = append(alerts, HealthAlert{
			Metric:    "orphaned_triage",
			Current:   float64(m.OrphanedTriageCount),
			Threshold: 3,
			Message:   "More than 3 triage sessions older than 1 hour have no bot comment and are not closed",
		})
	}

	return alerts
}
