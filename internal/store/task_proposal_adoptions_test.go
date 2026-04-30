package store

import (
	"database/sql"
	"testing"
)

func TestCreateAndReadTaskProposalAdoptions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	adoption := &TaskProposalAdoption{
		ProposalSnapshotID: "agentctl-create-repo-contract",
		Source:             "reconcile",
		ReportMode:         "read-only",
		RepoRef:            "agentctl",
		Repository:         "chaspy/agentctl",
		Tier:               "control-plane",
		Category:           "create_repo_contract",
		Title:              "Create repo contract",
		Objective:          "Add .agent/repo.yaml",
		TaskType:           "docs",
		Risk:               "low",
		ReviewPolicyRef:    "control-plane-review",
		ApprovalPolicyRef:  "self-hosting-approval",
		ApprovalStatus:     "not_required",
		ApprovalReason:     "docs proposal",
		DesiredOutcome:     []string{".agent/repo.yaml exists"},
		TriggerIssues:      []string{"repo contract not found: .agent/repo.yaml"},
		OperatorNote:       "queue for future AgentTask materialization",
	}
	if err := CreateTaskProposalAdoption(db, adoption); err != nil {
		t.Fatalf("CreateTaskProposalAdoption: %v", err)
	}
	if adoption.ID == 0 {
		t.Fatal("expected non-zero adoption id")
	}
	if adoption.Status != TaskProposalAdoptionStatusQueued {
		t.Fatalf("status = %q, want %q", adoption.Status, TaskProposalAdoptionStatusQueued)
	}

	count, err := CountQueuedTaskProposalAdoptions(db)
	if err != nil {
		t.Fatalf("CountQueuedTaskProposalAdoptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("queued adoption count = %d, want 1", count)
	}

	items, err := ListTaskProposalAdoptions(db)
	if err != nil {
		t.Fatalf("ListTaskProposalAdoptions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("adoptions len = %d, want 1", len(items))
	}
	if items[0].ProposalSnapshotID != "agentctl-create-repo-contract" {
		t.Fatalf("proposalSnapshotID = %q", items[0].ProposalSnapshotID)
	}
	if items[0].OperatorNote != "queue for future AgentTask materialization" {
		t.Fatalf("operatorNote = %q", items[0].OperatorNote)
	}

	got, err := GetTaskProposalAdoption(db, adoption.ID)
	if err != nil {
		t.Fatalf("GetTaskProposalAdoption: %v", err)
	}
	if got.ApprovalStatus != "not_required" {
		t.Fatalf("approvalStatus = %q", got.ApprovalStatus)
	}
	if len(got.DesiredOutcome) != 1 || got.DesiredOutcome[0] != ".agent/repo.yaml exists" {
		t.Fatalf("desiredOutcome mismatch: %+v", got.DesiredOutcome)
	}

	queued, err := GetQueuedTaskProposalAdoptionBySnapshotID(db, "agentctl-create-repo-contract")
	if err != nil {
		t.Fatalf("GetQueuedTaskProposalAdoptionBySnapshotID: %v", err)
	}
	if queued.ID != adoption.ID {
		t.Fatalf("queued adoption id = %d, want %d", queued.ID, adoption.ID)
	}
}

func TestGetQueuedTaskProposalAdoptionBySnapshotIDReturnsNoRows(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, err = GetQueuedTaskProposalAdoptionBySnapshotID(db, "missing")
	if err != sql.ErrNoRows {
		t.Fatalf("GetQueuedTaskProposalAdoptionBySnapshotID err = %v, want sql.ErrNoRows", err)
	}
}
