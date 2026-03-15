package store

import "testing"

func TestAgentSessionStages(t *testing.T) {
	stages := []string{StageNew, StageClarifying, StageResearching, StageReviewPending, StageRevision, StageApproved, StageComplete}
	if len(stages) != 7 {
		t.Fatalf("expected 7 stages, got %d", len(stages))
	}
}

func TestStageContextBrief(t *testing.T) {
	if StageContextBrief != "context_brief" {
		t.Fatalf("expected context_brief, got %s", StageContextBrief)
	}
}

func TestStaleSessionTypes(t *testing.T) {
	agent := StaleSession{ID: 1, ShadowRepo: "owner/shadow", ShadowIssueNumber: 10, SessionType: "agent"}
	triage := StaleSession{ID: 2, ShadowRepo: "owner/shadow", ShadowIssueNumber: 20, SessionType: "triage"}

	if agent.SessionType != "agent" {
		t.Fatalf("expected agent, got %s", agent.SessionType)
	}
	if triage.SessionType != "triage" {
		t.Fatalf("expected triage, got %s", triage.SessionType)
	}
	if agent.ShadowIssueNumber != 10 {
		t.Fatalf("expected 10, got %d", agent.ShadowIssueNumber)
	}
	if triage.ShadowRepo != "owner/shadow" {
		t.Fatalf("expected owner/shadow, got %s", triage.ShadowRepo)
	}
}

func TestApprovalGateTypes(t *testing.T) {
	gates := []string{GateClarification, GateResearch, GatePR, GatePromotePublic}
	if len(gates) != 4 {
		t.Fatalf("expected 4 gate types, got %d", len(gates))
	}
}

func TestApprovalStatuses(t *testing.T) {
	statuses := []string{ApprovalPending, ApprovalApproved, ApprovalRejected, ApprovalRevisionRequested}
	if len(statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(statuses))
	}
}
