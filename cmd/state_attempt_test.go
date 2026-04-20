package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateAttemptList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:       1,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		RepoMode:         "branch",
		SessionName:      "agentctl-agentctl-create-repo-contract",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Status:           "spawned",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateAttemptJSON
	var out bytes.Buffer
	stateAttemptListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAttemptJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}

	if err := runStateAttemptList(stateAttemptListCmd, nil); err != nil {
		t.Fatalf("runStateAttemptList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentctl-create-repo-contract") {
		t.Fatalf("expected attempt list to contain task name, got %q", got)
	}
	if !strings.Contains(got, "spawned") {
		t.Fatalf("expected attempt list to contain status, got %q", got)
	}
}

func TestRunStateAttemptGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:       1,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		RepoMode:         "branch",
		Branch:           "agentctl-create-repo-contract",
		SessionName:      "agentctl-agentctl-create-repo-contract",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Status:           "spawned",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateAttemptJSON
	var out bytes.Buffer
	stateAttemptGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAttemptJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAttemptJSON = true

	if err := runStateAttemptGet(stateAttemptGetCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateAttemptGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"agentTaskName": "agentctl-create-repo-contract"`) {
		t.Fatalf("expected attempt JSON to contain task name, got %q", got)
	}
	if !strings.Contains(got, `"status": "spawned"`) {
		t.Fatalf("expected attempt JSON to contain status, got %q", got)
	}
}

func TestRunStateAttemptOutcomePersistsSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	now := time.Now()
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "agentctl-create-repo-contract",
		RepoRef:    "agentctl",
		Repository: "chaspy/agentctl",
		Objective:  "Add .agent/repo.yaml",
		TaskType:   "docs",
		Risk:       "low",
		Status:     "spawned",
		SourceKind: "manifest",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		SelectionMode:    "auto_task_type",
		SelectedAgent:    "codex",
		SelectedRepoMode: "branch",
		Status:           "applied",
		RouteReason:      "task-type docs prefers codex",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:       1,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		RepoMode:         "branch",
		Branch:           "agentctl-create-repo-contract",
		SessionName:      "agentctl-agentctl-create-repo-contract",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Summary:          "Add repo contract and open PR",
		Status:           "spawned",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}
	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Agent:         "codex",
		Repository:    "agentctl",
		SessionID:     "zellij-agentctl-agentctl-create-repo-contract",
		CWD:           "/tmp/agentctl/worktree-agentctl-create-repo-contract",
		GitBranch:     "agentctl-create-repo-contract",
		ZellijSession: "agentctl-agentctl-create-repo-contract",
		Status:        "active",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		PRNumber:      42,
		PRURL:         "https://github.com/chaspy/agentctl/pull/42",
		PRState:       "OPEN",
		TaskSummary:   "Add repo contract and open PR",
		LastActive:    now,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origDryRun := stateAttemptOutcomeDryRun
	origStatus := stateAttemptOutcomeStatus
	origSummary := stateAttemptOutcomeSummary
	origPRNumber := stateAttemptOutcomePRNumber
	origPRURL := stateAttemptOutcomePRURL
	origPRState := stateAttemptOutcomePRState
	origCommitSHA := stateAttemptOutcomeCommitSHA
	origFailureCategory := stateAttemptOutcomeFailureCategory
	origFailureReason := stateAttemptOutcomeFailureReason
	origSource := stateAttemptOutcomeSource
	var out bytes.Buffer
	stateAttemptOutcomeCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAttemptOutcomeDryRun = origDryRun
		stateAttemptOutcomeStatus = origStatus
		stateAttemptOutcomeSummary = origSummary
		stateAttemptOutcomePRNumber = origPRNumber
		stateAttemptOutcomePRURL = origPRURL
		stateAttemptOutcomePRState = origPRState
		stateAttemptOutcomeCommitSHA = origCommitSHA
		stateAttemptOutcomeFailureCategory = origFailureCategory
		stateAttemptOutcomeFailureReason = origFailureReason
		stateAttemptOutcomeSource = origSource
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAttemptOutcomeDryRun = false
	stateAttemptOutcomeStatus = "completed"
	stateAttemptOutcomeSummary = ""
	stateAttemptOutcomePRNumber = 0
	stateAttemptOutcomePRURL = ""
	stateAttemptOutcomePRState = ""
	stateAttemptOutcomeCommitSHA = "abc123"
	stateAttemptOutcomeFailureCategory = ""
	stateAttemptOutcomeFailureReason = ""
	stateAttemptOutcomeSource = ""

	if err := runStateAttemptOutcome(stateAttemptOutcomeCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateAttemptOutcome: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Recorded outcome #1 from Attempt 1.") {
		t.Fatalf("expected success output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	outcome, err := store.GetAgentTaskOutcome(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskOutcome: %v", err)
	}
	if outcome == nil || outcome.PRURL != "https://github.com/chaspy/agentctl/pull/42" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.Status != "completed" || outcome.CommitSHA != "abc123" {
		t.Fatalf("unexpected outcome final state: %+v", outcome)
	}
	task, err := store.GetAgentTask(db, "agentctl-create-repo-contract")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status = %q", task.Status)
	}
	decision, err := store.GetAgentTaskDecision(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskDecision: %v", err)
	}
	if decision.Status != "completed" {
		t.Fatalf("decision status = %q", decision.Status)
	}
}
