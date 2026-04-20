package store

import "testing"

func TestCreateAndUpdateAgentTaskAttempt(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	attempt := &AgentTaskAttempt{
		DecisionID:     1,
		AgentTaskName:  "agentctl-create-repo-contract",
		RepoRef:        "agentctl",
		Repository:     "chaspy/agentctl",
		TaskType:       "docs",
		Risk:           "low",
		Agent:          "codex",
		RepoMode:       "branch",
		Branch:         "agentctl-create-repo-contract",
		SessionName:    "agentctl-agentctl-create-repo-contract",
		LaunchCommand:  "codex --dangerously-bypass-approvals-and-sandbox --no-alt-screen",
		InitialMessage: "Add .agent/repo.yaml",
		Summary:        "Add .agent/repo.yaml",
		Status:         "starting",
		FailureReason:  "",
	}
	if err := CreateAgentTaskAttempt(db, attempt); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}

	count, err := CountAgentTaskAttempts(db)
	if err != nil {
		t.Fatalf("CountAgentTaskAttempts: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	attempt.Status = "spawned"
	attempt.ManagedSessionID = "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract"
	attempt.WorkDir = "/tmp/agentctl/worktree-agentctl-create-repo-contract"
	if err := UpdateAgentTaskAttempt(db, attempt); err != nil {
		t.Fatalf("UpdateAgentTaskAttempt: %v", err)
	}

	got, err := GetAgentTaskAttempt(db, attempt.ID)
	if err != nil {
		t.Fatalf("GetAgentTaskAttempt: %v", err)
	}
	if got == nil {
		t.Fatal("expected attempt")
	}
	if got.Status != "spawned" {
		t.Fatalf("status = %q", got.Status)
	}
	if got.ManagedSessionID == "" {
		t.Fatal("expected managed session id")
	}
}
