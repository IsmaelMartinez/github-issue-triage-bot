package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateSession inserts a new agent session and returns its ID.
func (s *Store) CreateSession(ctx context.Context, sess AgentSession) (int64, error) {
	ctxJSON, err := json.Marshal(sess.Context)
	if err != nil {
		return 0, fmt.Errorf("marshal session context: %w", err)
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (repo, issue_number, shadow_repo, shadow_issue_number, stage, context, round_trip_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, sess.Repo, sess.IssueNumber, sess.ShadowRepo, sess.ShadowIssueNumber, sess.Stage, ctxJSON, sess.RoundTripCount).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// GetSession retrieves an agent session by repo and issue number.
func (s *Store) GetSession(ctx context.Context, repo string, issueNumber int) (*AgentSession, error) {
	var sess AgentSession
	var ctxJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, repo, issue_number, shadow_repo, shadow_issue_number, stage, context, round_trip_count, created_at, updated_at
		FROM agent_sessions
		WHERE repo = $1 AND issue_number = $2
	`, repo, issueNumber).Scan(
		&sess.ID, &sess.Repo, &sess.IssueNumber, &sess.ShadowRepo, &sess.ShadowIssueNumber,
		&sess.Stage, &ctxJSON, &sess.RoundTripCount, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if len(ctxJSON) > 0 {
		_ = json.Unmarshal(ctxJSON, &sess.Context)
	}
	return &sess, nil
}

// GetSessionByShadow retrieves an agent session by shadow repo and shadow issue number.
func (s *Store) GetSessionByShadow(ctx context.Context, shadowRepo string, shadowIssueNumber int) (*AgentSession, error) {
	var sess AgentSession
	var ctxJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, repo, issue_number, shadow_repo, shadow_issue_number, stage, context, round_trip_count, created_at, updated_at
		FROM agent_sessions
		WHERE shadow_repo = $1 AND shadow_issue_number = $2
	`, shadowRepo, shadowIssueNumber).Scan(
		&sess.ID, &sess.Repo, &sess.IssueNumber, &sess.ShadowRepo, &sess.ShadowIssueNumber,
		&sess.Stage, &ctxJSON, &sess.RoundTripCount, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session by shadow: %w", err)
	}
	if len(ctxJSON) > 0 {
		_ = json.Unmarshal(ctxJSON, &sess.Context)
	}
	return &sess, nil
}

// UpdateSessionStage updates the stage, context, and round-trip count for a session.
func (s *Store) UpdateSessionStage(ctx context.Context, id int64, stage string, sessionCtx map[string]any, roundTrips int) error {
	ctxJSON, err := json.Marshal(sessionCtx)
	if err != nil {
		return fmt.Errorf("marshal session context: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE agent_sessions
		SET stage = $1, context = $2, round_trip_count = $3, updated_at = now()
		WHERE id = $4
	`, stage, ctxJSON, roundTrips, id)
	return err
}

// StaleSession holds the minimum info needed to close a stale shadow issue.
type StaleSession struct {
	ID                int64
	ShadowRepo        string
	ShadowIssueNumber int
	SessionType       string // "agent" or "triage"
}

// ListStaleSessions returns agent and triage sessions with open shadow issues
// that haven't been acted on within the given duration.
func (s *Store) ListStaleSessions(ctx context.Context, staleDuration time.Duration) ([]StaleSession, error) {
	cutoff := time.Now().Add(-staleDuration)
	var results []StaleSession

	// Stale agent sessions (not in a terminal stage).
	// shadow_issue_number can be NULL when shadow issue creation failed mid-session; skip those rows.
	rows, err := s.pool.Query(ctx, `
		SELECT id, shadow_repo, shadow_issue_number
		FROM agent_sessions
		WHERE stage NOT IN ('complete') AND created_at < $1 AND shadow_issue_number IS NOT NULL
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale agent sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ss StaleSession
		if err := rows.Scan(&ss.ID, &ss.ShadowRepo, &ss.ShadowIssueNumber); err != nil {
			return nil, err
		}
		ss.SessionType = "agent"
		results = append(results, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Stale triage sessions (no corresponding bot_comment = not promoted, not already closed)
	rows2, err := s.pool.Query(ctx, `
		SELECT t.id, t.shadow_repo, t.shadow_issue_number
		FROM triage_sessions t
		LEFT JOIN bot_comments b ON t.repo = b.repo AND t.issue_number = b.issue_number
		WHERE b.id IS NULL AND t.closed_at IS NULL AND t.created_at < $1
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale triage sessions: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var ss StaleSession
		if err := rows2.Scan(&ss.ID, &ss.ShadowRepo, &ss.ShadowIssueNumber); err != nil {
			return nil, err
		}
		ss.SessionType = "triage"
		results = append(results, ss)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// MarkSessionComplete sets an agent session's stage to "complete".
func (s *Store) MarkSessionComplete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET stage = $1, updated_at = now() WHERE id = $2
	`, StageComplete, id)
	return err
}

// MarkTriageSessionClosed sets the closed_at timestamp on a triage session.
func (s *Store) MarkTriageSessionClosed(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE triage_sessions SET closed_at = now() WHERE id = $1
	`, id)
	return err
}

// CreateAuditEntry inserts a new audit log entry.
func (s *Store) CreateAuditEntry(ctx context.Context, entry AuditEntry) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_audit_log (session_id, action_type, input_hash, output_summary, safety_check_passed, confidence_score)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, entry.SessionID, entry.ActionType, entry.InputHash, entry.OutputSummary, entry.SafetyCheckPassed, entry.ConfidenceScore)
	return err
}

// CreateApprovalGate inserts a new approval gate and returns its ID.
func (s *Store) CreateApprovalGate(ctx context.Context, gate ApprovalGate) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO approval_gates (session_id, gate_type, status)
		VALUES ($1, $2, $3)
		RETURNING id
	`, gate.SessionID, gate.GateType, gate.Status).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create approval gate: %w", err)
	}
	return id, nil
}

// ResolveApprovalGate updates the status, approver, and resolved_at timestamp of an approval gate.
func (s *Store) ResolveApprovalGate(ctx context.Context, id int64, status string, approver string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE approval_gates
		SET status = $1, approver = $2, resolved_at = $3
		WHERE id = $4
	`, status, approver, time.Now(), id)
	return err
}

// GetPendingGate retrieves the oldest pending approval gate for a session.
func (s *Store) GetPendingGate(ctx context.Context, sessionID int64) (*ApprovalGate, error) {
	var gate ApprovalGate
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, gate_type, status, approver, created_at, resolved_at
		FROM approval_gates
		WHERE session_id = $1 AND status = $2
		ORDER BY created_at ASC
		LIMIT 1
	`, sessionID, ApprovalPending).Scan(
		&gate.ID, &gate.SessionID, &gate.GateType, &gate.Status, &gate.Approver, &gate.CreatedAt, &gate.ResolvedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pending gate: %w", err)
	}
	return &gate, nil
}
