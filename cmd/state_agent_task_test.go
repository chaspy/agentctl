package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/provider"
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

func TestRunStateAgentTaskDecideDryRun(t *testing.T) {
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
	origDryRun := stateAgentTaskDecideDryRun
	origLookupExecutable := lookupExecutable
	origClaudeRateFn := claudeRateFn
	origCodexRateFn := codexRateFn
	var out bytes.Buffer
	stateAgentTaskDecideCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAgentTaskDecideDryRun = origDryRun
		lookupExecutable = origLookupExecutable
		claudeRateFn = origClaudeRateFn
		codexRateFn = origCodexRateFn
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAgentTaskDecideDryRun = true
	lookupExecutable = func(string) (string, error) { return "/usr/bin/fake", nil }
	claudeRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentClaude, RemainingPct: 40}, nil
	}
	codexRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentCodex, RemainingPct: 60}, nil
	}

	if err := runStateAgentTaskDecide(stateAgentTaskDecideCmd, []string{"agentctl-create-repo-contract-adoption-1"}); err != nil {
		t.Fatalf("runStateAgentTaskDecide: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "AgentTask decision (dry-run)") {
		t.Fatalf("expected dry-run output, got %q", got)
	}
	if !strings.Contains(got, "selected_agent") || !strings.Contains(got, "codex") {
		t.Fatalf("expected selected codex in output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	decisions, err := store.ListAgentTaskDecisions(db)
	if err != nil {
		t.Fatalf("ListAgentTaskDecisions: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected no decisions after dry-run, got %d", len(decisions))
	}
	task, err := store.GetAgentTask(db, "agentctl-create-repo-contract-adoption-1")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "planned" {
		t.Fatalf("status = %q, want planned", task.Status)
	}
}

func TestRunStateAgentTaskDecidePersistsDecision(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:             "agentctl-create-repo-contract-adoption-1",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		Objective:        "Add .agent/repo.yaml",
		TaskType:         "docs",
		Risk:             "low",
		RoutingPolicyRef: "control-plane-default",
		SourceKind:       "proposal_adoption",
		SourceRef:        "1",
		Status:           "planned",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origDryRun := stateAgentTaskDecideDryRun
	origLookupExecutable := lookupExecutable
	origClaudeRateFn := claudeRateFn
	origCodexRateFn := codexRateFn
	var out bytes.Buffer
	stateAgentTaskDecideCmd.SetOut(&out)
	t.Cleanup(func() {
		stateAgentTaskDecideDryRun = origDryRun
		lookupExecutable = origLookupExecutable
		claudeRateFn = origClaudeRateFn
		codexRateFn = origCodexRateFn
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateAgentTaskDecideDryRun = false
	lookupExecutable = func(string) (string, error) { return "/usr/bin/fake", nil }
	claudeRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentClaude, RemainingPct: 40}, nil
	}
	codexRateFn = func() (provider.RateInfo, error) {
		return provider.RateInfo{Agent: provider.AgentCodex, RemainingPct: 60}, nil
	}

	if err := runStateAgentTaskDecide(stateAgentTaskDecideCmd, []string{"agentctl-create-repo-contract-adoption-1"}); err != nil {
		t.Fatalf("runStateAgentTaskDecide: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Recorded decision #") {
		t.Fatalf("expected recorded output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	decisions, err := store.ListAgentTaskDecisions(db)
	if err != nil {
		t.Fatalf("ListAgentTaskDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].SelectedAgent != "codex" {
		t.Fatalf("selected_agent = %q", decisions[0].SelectedAgent)
	}
	if decisions[0].PolicyVersion != "unresolved" {
		t.Fatalf("policy_version = %q", decisions[0].PolicyVersion)
	}
	task, err := store.GetAgentTask(db, "agentctl-create-repo-contract-adoption-1")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "routed" {
		t.Fatalf("status = %q, want routed", task.Status)
	}
}
