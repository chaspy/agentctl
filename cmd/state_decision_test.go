package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func prepareTestRepoHome(t *testing.T, root, repository string) string {
	t.Helper()
	home := filepath.Join(root, "home")
	repoPath := filepath.Join(home, "go", "src", "github.com", filepath.FromSlash(repository))
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(repoPath): %v", err)
	}
	t.Setenv("HOME", home)
	return repoPath
}

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

func TestRunStateDecisionAttemptDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	prepareTestRepoHome(t, tmpDir, "chaspy/agentctl")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:           "agentctl-create-repo-contract",
		RepoRef:        "agentctl",
		Repository:     "chaspy/agentctl",
		Objective:      "Add .agent/repo.yaml",
		TaskType:       "docs",
		Risk:           "low",
		DesiredOutcome: []string{".agent/repo.yaml exists"},
		Status:         "routed",
		SourceKind:     "manifest",
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
		Status:           "recorded",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origDryRun := stateDecisionAttemptDryRun
	origBranch := stateDecisionAttemptBranch
	origName := stateDecisionAttemptName
	origMessage := stateDecisionAttemptMessage
	origSummary := stateDecisionAttemptSummary
	var out bytes.Buffer
	stateDecisionAttemptCmd.SetOut(&out)
	t.Cleanup(func() {
		stateDecisionAttemptDryRun = origDryRun
		stateDecisionAttemptBranch = origBranch
		stateDecisionAttemptName = origName
		stateDecisionAttemptMessage = origMessage
		stateDecisionAttemptSummary = origSummary
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateDecisionAttemptDryRun = true
	stateDecisionAttemptBranch = ""
	stateDecisionAttemptName = ""
	stateDecisionAttemptMessage = ""
	stateDecisionAttemptSummary = ""

	if err := runStateDecisionAttempt(stateDecisionAttemptCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateDecisionAttempt: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "AgentTask attempt (dry-run)") {
		t.Fatalf("expected dry-run output, got %q", got)
	}
	if !strings.Contains(got, "agentctl-create-repo-contract") {
		t.Fatalf("expected task name in output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	attempts, err := store.ListAgentTaskAttempts(db)
	if err != nil {
		t.Fatalf("ListAgentTaskAttempts: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected no attempts after dry-run, got %d", len(attempts))
	}
}

func TestRunStateDecisionAttemptPersistsSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")
	prepareTestRepoHome(t, tmpDir, "chaspy/agentctl")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "agentctl-create-repo-contract",
		RepoRef:    "agentctl",
		Repository: "chaspy/agentctl",
		Objective:  "Add .agent/repo.yaml",
		TaskType:   "docs",
		Risk:       "low",
		Status:     "routed",
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
		Status:           "recorded",
		RouteReason:      "task-type docs prefers codex",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origDryRun := stateDecisionAttemptDryRun
	origBranch := stateDecisionAttemptBranch
	origName := stateDecisionAttemptName
	origMessage := stateDecisionAttemptMessage
	origSummary := stateDecisionAttemptSummary
	origAttach := stateDecisionAttemptAttach
	origRunner := runAgentTaskAttemptSpawn
	var out bytes.Buffer
	stateDecisionAttemptCmd.SetOut(&out)
	t.Cleanup(func() {
		stateDecisionAttemptDryRun = origDryRun
		stateDecisionAttemptBranch = origBranch
		stateDecisionAttemptName = origName
		stateDecisionAttemptMessage = origMessage
		stateDecisionAttemptSummary = origSummary
		stateDecisionAttemptAttach = origAttach
		runAgentTaskAttemptSpawn = origRunner
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	runAgentTaskAttemptSpawn = func(plan *agentTaskAttemptPlan) (*spawnExecutionResult, error) {
		return &spawnExecutionResult{
			SessionDBID:   "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
			SessionName:   plan.Attempt.SessionName,
			WorkDir:       "/tmp/agentctl/worktree-agentctl-create-repo-contract",
			GitBranch:     plan.Attempt.Branch,
			LaunchCommand: plan.Attempt.LaunchCommand,
		}, nil
	}

	if err := runStateDecisionAttempt(stateDecisionAttemptCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateDecisionAttempt: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Executed attempt #1 from Decision 1.") {
		t.Fatalf("expected success output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	attempt, err := store.GetAgentTaskAttempt(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskAttempt: %v", err)
	}
	if attempt == nil || attempt.Status != "spawned" {
		t.Fatalf("attempt status = %+v", attempt)
	}
	task, err := store.GetAgentTask(db, "agentctl-create-repo-contract")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "spawned" {
		t.Fatalf("task status = %q", task.Status)
	}
	decision, err := store.GetAgentTaskDecision(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskDecision: %v", err)
	}
	if decision.Status != "applied" {
		t.Fatalf("decision status = %q", decision.Status)
	}
}

func TestRunStateDecisionAttemptAttachSession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "myassistant-pr-followup",
		RepoRef:    "myassistant",
		Repository: "chaspy/myassistant",
		Objective:  "Attach an existing PR worker",
		TaskType:   "docs",
		Risk:       "low",
		Status:     "routed",
		SourceKind: "manifest",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:    "myassistant-pr-followup",
		RepoRef:          "myassistant",
		Repository:       "chaspy/myassistant",
		TaskType:         "docs",
		Risk:             "low",
		SelectionMode:    "explicit",
		SelectedAgent:    "codex",
		SelectedRepoMode: "branch",
		Status:           "recorded",
		RouteReason:      "explicit codex selection",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	if err := store.UpsertSession(db, &store.Session{
		ID:             "codex:chaspy/myassistant:zellij-existing-pr-worker",
		Agent:          "codex",
		Repository:     "chaspy/myassistant",
		SessionID:      "zellij-existing-pr-worker",
		CWD:            "/tmp/myassistant/worktree-existing-pr-worker",
		GitBranch:      "feat/existing-pr-worker",
		ZellijSession:  "existing-pr-worker",
		Status:         "idle",
		DesiredState:   store.DesiredStateRunning,
		RuntimeStatus:  "running",
		LifecycleState: store.LifecycleStateRunning,
		TaskSummary:    "Existing PR worker",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origDryRun := stateDecisionAttemptDryRun
	origBranch := stateDecisionAttemptBranch
	origName := stateDecisionAttemptName
	origMessage := stateDecisionAttemptMessage
	origSummary := stateDecisionAttemptSummary
	origAttach := stateDecisionAttemptAttach
	origRunner := runAgentTaskAttemptSpawn
	var out bytes.Buffer
	stateDecisionAttemptCmd.SetOut(&out)
	t.Cleanup(func() {
		stateDecisionAttemptDryRun = origDryRun
		stateDecisionAttemptBranch = origBranch
		stateDecisionAttemptName = origName
		stateDecisionAttemptMessage = origMessage
		stateDecisionAttemptSummary = origSummary
		stateDecisionAttemptAttach = origAttach
		runAgentTaskAttemptSpawn = origRunner
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateDecisionAttemptDryRun = false
	stateDecisionAttemptAttach = "existing-pr-worker"
	runAgentTaskAttemptSpawn = func(plan *agentTaskAttemptPlan) (*spawnExecutionResult, error) {
		t.Fatal("spawn runner should not be called for --attach-session")
		return nil, nil
	}

	if err := runStateDecisionAttempt(stateDecisionAttemptCmd, []string{"1"}); err != nil {
		t.Fatalf("runStateDecisionAttempt: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Attached session existing-pr-worker as attempt #1 from Decision 1.") {
		t.Fatalf("expected attach output, got %q", got)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(second): %v", err)
	}
	defer db.Close()
	attempt, err := store.GetAgentTaskAttempt(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskAttempt: %v", err)
	}
	if attempt == nil || attempt.ManagedSessionID != "codex:chaspy/myassistant:zellij-existing-pr-worker" {
		t.Fatalf("attempt mismatch: %+v", attempt)
	}
	if attempt.LaunchCommand != "attached-existing-session" || attempt.Status != "spawned" {
		t.Fatalf("attempt attach fields mismatch: %+v", attempt)
	}
	task, err := store.GetAgentTask(db, "myassistant-pr-followup")
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}
	if task.Status != "spawned" {
		t.Fatalf("task status = %q", task.Status)
	}
	decision, err := store.GetAgentTaskDecision(db, 1)
	if err != nil {
		t.Fatalf("GetAgentTaskDecision: %v", err)
	}
	if decision.Status != "applied" {
		t.Fatalf("decision status = %q", decision.Status)
	}
}
