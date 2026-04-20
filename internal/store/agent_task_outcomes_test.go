package store

import "testing"

func TestAgentTaskOutcomeCRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	outcome := &AgentTaskOutcome{
		AttemptID:        1,
		DecisionID:       2,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		Branch:           "agentctl-create-repo-contract",
		SessionName:      "agentctl-agentctl-create-repo-contract",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Status:           "completed",
		ResultSummary:    "Opened PR for repo contract",
		PRNumber:         42,
		PRURL:            "https://github.com/chaspy/agentctl/pull/42",
		PRState:          "OPEN",
		CommitSHA:        "abc123",
		Source:           "session",
	}
	if err := CreateAgentTaskOutcome(db, outcome); err != nil {
		t.Fatalf("CreateAgentTaskOutcome: %v", err)
	}
	if outcome.ID == 0 {
		t.Fatalf("expected ID to be assigned")
	}

	list, err := ListAgentTaskOutcomes(db)
	if err != nil {
		t.Fatalf("ListAgentTaskOutcomes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].PRURL != "https://github.com/chaspy/agentctl/pull/42" {
		t.Fatalf("PRURL = %q", list[0].PRURL)
	}

	got, err := GetAgentTaskOutcome(db, outcome.ID)
	if err != nil {
		t.Fatalf("GetAgentTaskOutcome: %v", err)
	}
	if got == nil || got.Status != "completed" {
		t.Fatalf("got = %+v", got)
	}

	byAttempt, err := GetAgentTaskOutcomeByAttemptID(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskOutcomeByAttemptID: %v", err)
	}
	if byAttempt == nil || byAttempt.AttemptID != 1 {
		t.Fatalf("byAttempt = %+v", byAttempt)
	}

	count, err := CountAgentTaskOutcomes(db)
	if err != nil {
		t.Fatalf("CountAgentTaskOutcomes: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
