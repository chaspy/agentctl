package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
