package store

import "testing"

func TestAgentSessionStages(t *testing.T) {
	stages := []string{StageNew, StageClarifying, StageResearching, StageReviewPending, StageRevision, StageApproved, StageComplete}
	if len(stages) != 7 {
		t.Fatalf("expected 7 stages, got %d", len(stages))
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
