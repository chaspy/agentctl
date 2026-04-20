package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateAgentTaskList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "agentctl-create-repo-contract-adoption-1",
		RepoRef:    "agentctl",
		Repository: "chaspy/agentctl",
		Objective:  "Add .agent/repo.yaml",
		TaskType:   "docs",
		Risk:       "low",
		SourceKind: "proposal_adoption",
		SourceRef:  "1",
		Status:     "planned",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateAgentTaskJSON
	var out bytes.Buffer
	stateAgentTaskListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAgentTaskJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAgentTaskJSON = false

	if err := runStateAgentTaskList(stateAgentTaskListCmd, nil); err != nil {
		t.Fatalf("runStateAgentTaskList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentctl-create-repo-contract-adoption-1") {
		t.Fatalf("expected list output to contain agent task name, got %q", got)
	}
	if !strings.Contains(got, "proposal_adoption") {
		t.Fatalf("expected list output to contain source kind, got %q", got)
	}
}

func TestRunStateAgentTaskGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:                        "agentctl-create-repo-contract-adoption-1",
		RepoRef:                     "agentctl",
		Repository:                  "chaspy/agentctl",
		Objective:                   "Add .agent/repo.yaml",
		TaskType:                    "docs",
		Risk:                        "low",
		ContextRefs:                 []string{"agentops-vision"},
		ApprovalRequiredBeforeMerge: false,
		SourceKind:                  "proposal_adoption",
		SourceRef:                   "1",
		Status:                      "planned",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateAgentTaskJSON
	var out bytes.Buffer
	stateAgentTaskGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAgentTaskJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAgentTaskJSON = true

	if err := runStateAgentTaskGet(stateAgentTaskGetCmd, []string{"agentctl-create-repo-contract-adoption-1"}); err != nil {
		t.Fatalf("runStateAgentTaskGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"name": "agentctl-create-repo-contract-adoption-1"`) {
		t.Fatalf("expected JSON output to contain agent task name, got %q", got)
	}
	if !strings.Contains(got, `"sourceKind": "proposal_adoption"`) {
		t.Fatalf("expected JSON output to contain source kind, got %q", got)
	}
}
