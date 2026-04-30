package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateOutcomeList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.CreateAgentTaskOutcome(db, &store.AgentTaskOutcome{
		AttemptID:     1,
		DecisionID:    2,
		AgentTaskName: "agentctl-create-repo-contract",
		RepoRef:       "agentctl",
		Repository:    "chaspy/agentctl",
		TaskType:      "docs",
		Risk:          "low",
		Agent:         "codex",
		Status:        "completed",
		PRURL:         "https://github.com/chaspy/agentctl/pull/42",
		Source:        "session",
	}); err != nil {
		t.Fatalf("CreateAgentTaskOutcome: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateOutcomeJSON
	var out bytes.Buffer
	stateOutcomeListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateOutcomeJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateOutcomeJSON = false

	if err := runStateOutcomeList(stateOutcomeListCmd, nil); err != nil {
		t.Fatalf("runStateOutcomeList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentctl-create-repo-contract") {
		t.Fatalf("expected outcome list to contain task name, got %q", got)
	}
	if !strings.Contains(got, "completed") {
		t.Fatalf("expected outcome list to contain status, got %q", got)
	}
}

func TestRunStateOutcomeGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.CreateAgentTaskOutcome(db, &store.AgentTaskOutcome{
		AttemptID:        1,
		DecisionID:       2,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Status:           "failed",
		FailureCategory:  "ci",
		FailureReason:    "tests failed",
		Source:           "manual",
	}); err != nil {
		t.Fatalf("CreateAgentTaskOutcome: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateOutcomeJSON
	var out bytes.Buffer
	stateOutcomeGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateOutcomeJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateOutcomeJSON = true

	if err := runStateOutcomeGet(stateOutcomeGetCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateOutcomeGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"agentTaskName": "agentctl-create-repo-contract"`) {
		t.Fatalf("expected outcome JSON to contain task name, got %q", got)
	}
	if !strings.Contains(got, `"failureCategory": "ci"`) {
		t.Fatalf("expected outcome JSON to contain failure category, got %q", got)
	}
}
