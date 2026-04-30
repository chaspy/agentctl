package store

import "testing"

func TestReplaceAndReadTaskProposalSnapshots(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	proposals := []TaskProposalSnapshot{
		{
			ID:                "agentctl-create-repo-contract",
			Source:            "reconcile",
			ReportMode:        "read-only",
			RepoRef:           "agentctl",
			Repository:        "chaspy/agentctl",
			Tier:              "control-plane",
			Category:          "create_repo_contract",
			Title:             "Create repo contract",
			Objective:         "Add .agent/repo.yaml",
			TaskType:          "docs",
			Risk:              "low",
			ReviewPolicyRef:   "control-plane-review",
			ApprovalPolicyRef: "self-hosting-approval",
			ApprovalStatus:    "not_required",
			ApprovalReason:    "docs proposal",
			DesiredOutcome:    []string{"repo contract exists"},
			TriggerIssues:     []string{"repo contract not found"},
			RawProposalJSON:   `{"id":"agentctl-create-repo-contract"}`,
		},
	}
	if err := ReplaceTaskProposalSnapshots(db, "reconcile", proposals); err != nil {
		t.Fatalf("ReplaceTaskProposalSnapshots: %v", err)
	}

	count, err := CountTaskProposalSnapshots(db)
	if err != nil {
		t.Fatalf("CountTaskProposalSnapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	list, err := ListTaskProposalSnapshots(db)
	if err != nil {
		t.Fatalf("ListTaskProposalSnapshots: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].RepoRef != "agentctl" || list[0].ApprovalStatus != "not_required" {
		t.Fatalf("unexpected list row: %+v", list[0])
	}
	if len(list[0].DesiredOutcome) != 1 || list[0].DesiredOutcome[0] != "repo contract exists" {
		t.Fatalf("desired outcome mismatch: %+v", list[0].DesiredOutcome)
	}

	got, err := GetTaskProposalSnapshot(db, "agentctl-create-repo-contract")
	if err != nil {
		t.Fatalf("GetTaskProposalSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("expected proposal snapshot")
	}
	if got.Category != "create_repo_contract" {
		t.Fatalf("category = %q, want create_repo_contract", got.Category)
	}

	if err := ReplaceTaskProposalSnapshots(db, "reconcile", nil); err != nil {
		t.Fatalf("ReplaceTaskProposalSnapshots(clear): %v", err)
	}
	count, err = CountTaskProposalSnapshots(db)
	if err != nil {
		t.Fatalf("CountTaskProposalSnapshots after clear: %v", err)
	}
	if count != 0 {
		t.Fatalf("count after clear = %d, want 0", count)
	}
}
