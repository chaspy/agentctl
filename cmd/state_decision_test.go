package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateDecisionList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
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
		RouteReason:      "task-type docs prefers codex",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateDecisionJSON
	var out bytes.Buffer
	stateDecisionListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateDecisionJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateDecisionJSON = false

	if err := runStateDecisionList(stateDecisionListCmd, nil); err != nil {
		t.Fatalf("runStateDecisionList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentctl-create-repo-contract") {
		t.Fatalf("expected decision list to contain task name, got %q", got)
	}
	if !strings.Contains(got, "codex") {
		t.Fatalf("expected decision list to contain selected agent, got %q", got)
	}
}
