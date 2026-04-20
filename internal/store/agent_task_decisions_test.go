package store

import "testing"

func TestCreateAndReadAgentTaskDecision(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	decision := &AgentTaskDecision{
		AgentTaskName:     "agentctl-create-repo-contract",
		RepoRef:           "agentctl",
		Repository:        "chaspy/agentctl",
		TaskType:          "docs",
		Risk:              "low",
		RoutingPolicyRef:  "control-plane-default",
		PolicyVersion:     "unresolved",
		SelectionMode:     "auto_task_type",
		SelectedAgent:     "codex",
		SelectedRepoMode:  "branch",
		RepoProfileSource: "managed_repo",
		ModeSource:        "default",
		AgentSource:       "default",
		EligibleAgents:    []string{"claude", "codex"},
		CandidateScores: []AgentTaskDecisionCandidate{
			{Agent: "codex", Score: 75, Available: true, Reason: "docs preferred"},
			{Agent: "claude", Score: 60, Available: true, Reason: "fallback"},
		},
		RouteReason: "task-type docs prefers codex",
	}
	if err := CreateAgentTaskDecision(db, decision); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}

	count, err := CountAgentTaskDecisions(db)
	if err != nil {
		t.Fatalf("CountAgentTaskDecisions: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	got, err := GetAgentTaskDecision(db, decision.ID)
	if err != nil {
		t.Fatalf("GetAgentTaskDecision: %v", err)
	}
	if got == nil {
		t.Fatal("expected decision")
	}
	if got.SelectedAgent != "codex" {
		t.Fatalf("selectedAgent = %q", got.SelectedAgent)
	}
	if len(got.CandidateScores) != 2 {
		t.Fatalf("candidateScores len = %d, want 2", len(got.CandidateScores))
	}
}
