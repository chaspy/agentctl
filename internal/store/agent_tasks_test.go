package store

import "testing"

func TestUpsertAndReadAgentTask(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	task := &AgentTask{
		Name:                        "agentctl-create-repo-contract-adoption-1",
		RepoRef:                     "agentctl",
		Repository:                  "chaspy/agentctl",
		Objective:                   "Add .agent/repo.yaml to chaspy/agentctl and describe the repo contract for agentctl.",
		TaskType:                    "docs",
		Risk:                        "low",
		ContextRefs:                 []string{"agentops-vision"},
		DesiredOutcome:              []string{".agent/repo.yaml exists"},
		RoutingPolicyRef:            "control-plane-default",
		ReviewPolicyRef:             "control-plane-review",
		ApprovalPolicyRef:           "self-hosting-approval",
		ApprovalRequiredBeforeMerge: false,
		SourceKind:                  "proposal_adoption",
		SourceRef:                   "1",
		Status:                      "planned",
	}
	if err := UpsertAgentTask(db, task); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}

	count, err := CountAgentTasks(db)
	if err != nil {
		t.Fatalf("CountAgentTasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	got, err := GetAgentTask(db, task.Name)
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent task")
	}
	if got.SourceKind != "proposal_adoption" {
		t.Fatalf("sourceKind = %q", got.SourceKind)
	}
	if len(got.ContextRefs) != 1 || got.ContextRefs[0] != "agentops-vision" {
		t.Fatalf("contextRefs mismatch: %+v", got.ContextRefs)
	}
}
