package cmd

import (
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestBuildStateShowReportSummarizesHealth(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "myassistant",
		Repository:  "chaspy/myassistant",
		Role:        "personal-ops-console",
		Visibility:  "private",
		RawSpecJSON: `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.ReplaceTaskProposalSnapshots(db, "reconcile", []store.TaskProposalSnapshot{
		{
			ID:             "myassistant-create-repo-contract",
			RepoRef:        "myassistant",
			Repository:     "chaspy/myassistant",
			Category:       "create_repo_contract",
			Title:          "Create repo contract",
			Objective:      "Add .agent/repo.yaml",
			TaskType:       "docs",
			Risk:           "low",
			ApprovalStatus: "not_required",
		},
	}); err != nil {
		t.Fatalf("ReplaceTaskProposalSnapshots: %v", err)
	}
	if err := store.CreateTaskProposalAdoption(db, &store.TaskProposalAdoption{
		ProposalSnapshotID: "myassistant-create-repo-contract",
		Source:             "reconcile",
		ReportMode:         "read-only",
		RepoRef:            "myassistant",
		Repository:         "chaspy/myassistant",
		Category:           "create_repo_contract",
		Title:              "Create repo contract",
		Objective:          "Add .agent/repo.yaml",
		TaskType:           "docs",
		Risk:               "low",
		ApprovalStatus:     "not_required",
		OperatorNote:       "carry forward after review",
	}); err != nil {
		t.Fatalf("CreateTaskProposalAdoption: %v", err)
	}
	if err := store.UpsertAgentTask(db, &store.AgentTask{
		Name:       "myassistant-create-repo-contract-adoption-1",
		RepoRef:    "myassistant",
		Repository: "chaspy/myassistant",
		Objective:  "Add .agent/repo.yaml",
		TaskType:   "docs",
		Risk:       "low",
		SourceKind: "proposal_adoption",
		SourceRef:  "1",
		Status:     "planned",
	}); err != nil {
		t.Fatalf("UpsertAgentTask: %v", err)
	}
	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:     "myassistant-create-repo-contract-adoption-1",
		RepoRef:           "myassistant",
		Repository:        "chaspy/myassistant",
		TaskType:          "docs",
		Risk:              "low",
		SelectionMode:     "auto_task_type",
		SelectedAgent:     "codex",
		SelectedRepoMode:  "branch",
		RepoProfileSource: "managed_repo",
		ModeSource:        "managed_repo",
		AgentSource:       "default",
		EligibleAgents:    []string{"codex", "claude"},
		RouteReason:       "task-type docs prefers codex",
		Status:            "recorded",
	}); err != nil {
		t.Fatalf("CreateAgentTaskDecision: %v", err)
	}
	if err := store.CreateAgentTaskAttempt(db, &store.AgentTaskAttempt{
		DecisionID:       1,
		AgentTaskName:    "myassistant-create-repo-contract-adoption-1",
		RepoRef:          "myassistant",
		Repository:       "chaspy/myassistant",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		RepoMode:         "branch",
		SessionName:      "myassistant-myassistant-create-repo-contract",
		ManagedSessionID: "codex:chaspy/myassistant:zellij-myassistant-myassistant-create-repo-contract",
		Status:           "spawned",
	}); err != nil {
		t.Fatalf("CreateAgentTaskAttempt: %v", err)
	}
	if err := store.CreateAgentTaskOutcome(db, &store.AgentTaskOutcome{
		AttemptID:        1,
		DecisionID:       1,
		AgentTaskName:    "myassistant-create-repo-contract-adoption-1",
		RepoRef:          "myassistant",
		Repository:       "chaspy/myassistant",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		SessionName:      "myassistant-myassistant-create-repo-contract",
		ManagedSessionID: "codex:chaspy/myassistant:zellij-myassistant-myassistant-create-repo-contract",
		Status:           "completed",
		PRURL:            "https://github.com/chaspy/myassistant/pull/12",
		Source:           "session",
	}); err != nil {
		t.Fatalf("CreateAgentTaskOutcome: %v", err)
	}
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:blocked",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "blocked",
		Status:        "blocked",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "blocked-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:error",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "error",
		Status:        "error",
		Alive:         false,
		RuntimeStatus: "gone",
		ZellijSession: "error-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:ghost",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "ghost",
		Status:        "idle",
		Alive:         true,
		RuntimeStatus: "gone",
		ZellijSession: "ghost-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "claude:a/b:dup1",
		Agent:         "claude",
		Repository:    "a/b",
		SessionID:     "dup1",
		Status:        "idle",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "dup-session",
		LastActive:    now,
	})
	_ = store.UpsertSession(db, &store.Session{
		ID:            "codex:a/b:dup2",
		Agent:         "codex",
		Repository:    "a/b",
		SessionID:     "dup2",
		Status:        "blocked",
		BlockedReason: "rate_limit",
		Alive:         true,
		RuntimeStatus: "running",
		ZellijSession: "dup-session",
		LastActive:    now,
	})

	report, err := buildStateShowReport(db)
	if err != nil {
		t.Fatalf("buildStateShowReport: %v", err)
	}

	if report.Summary.ActiveSessions != 5 {
		t.Fatalf("active_sessions = %d, want 5", report.Summary.ActiveSessions)
	}
	if report.Summary.ManagedRepos != 1 {
		t.Fatalf("managed_repos = %d, want 1", report.Summary.ManagedRepos)
	}
	if report.Summary.TaskProposals != 1 {
		t.Fatalf("task_proposals = %d, want 1", report.Summary.TaskProposals)
	}
	if report.Summary.QueuedProposalAdoptions != 1 {
		t.Fatalf("queued_proposal_adoptions = %d, want 1", report.Summary.QueuedProposalAdoptions)
	}
	if report.Summary.AgentTasks != 1 {
		t.Fatalf("agent_tasks = %d, want 1", report.Summary.AgentTasks)
	}
	if report.Summary.AgentTaskDecisions != 1 {
		t.Fatalf("agent_task_decisions = %d, want 1", report.Summary.AgentTaskDecisions)
	}
	if report.Summary.AgentTaskAttempts != 1 {
		t.Fatalf("agent_task_attempts = %d, want 1", report.Summary.AgentTaskAttempts)
	}
	if report.Summary.AgentTaskOutcomes != 1 {
		t.Fatalf("agent_task_outcomes = %d, want 1", report.Summary.AgentTaskOutcomes)
	}
	if report.Summary.BlockedSessions != 2 {
		t.Fatalf("blocked_sessions = %d, want 2", report.Summary.BlockedSessions)
	}
	if report.Summary.ErrorSessions != 1 {
		t.Fatalf("error_sessions = %d, want 1", report.Summary.ErrorSessions)
	}
	if report.Summary.GhostSessions != 1 {
		t.Fatalf("ghost_sessions = %d, want 1", report.Summary.GhostSessions)
	}
	if report.Summary.DuplicateGroups != 1 {
		t.Fatalf("duplicate_groups = %d, want 1", report.Summary.DuplicateGroups)
	}
	if report.Summary.DuplicateSessions != 2 {
		t.Fatalf("duplicate_sessions = %d, want 2", report.Summary.DuplicateSessions)
	}

	var blocked, errorSess, ghost, dup1, dup2 *stateShowSession
	for i := range report.Sessions {
		s := &report.Sessions[i]
		switch s.ID {
		case "claude:a/b:blocked":
			blocked = s
		case "claude:a/b:error":
			errorSess = s
		case "claude:a/b:ghost":
			ghost = s
		case "claude:a/b:dup1":
			dup1 = s
		case "codex:a/b:dup2":
			dup2 = s
		}
	}

	if blocked == nil || errorSess == nil || ghost == nil || dup1 == nil || dup2 == nil {
		t.Fatalf("missing one or more sessions in report: %+v", report.Sessions)
	}
	if blocked.Duplicate || blocked.Ghost {
		t.Fatalf("blocked session flags unexpected: %+v", blocked)
	}
	if len(errorSess.Health) != 1 || errorSess.Health[0] != "error" {
		t.Fatalf("error session health unexpected: %+v", errorSess.Health)
	}
	if errorSess.Ghost {
		t.Fatalf("error session should not be ghost: %+v", errorSess)
	}
	if errorSess.Duplicate {
		t.Fatalf("error session should not be duplicate: %+v", errorSess)
	}
	if !ghost.Ghost {
		t.Fatalf("ghost session should be flagged ghost: %+v", ghost)
	}
	if !dup1.Duplicate || !dup2.Duplicate {
		t.Fatalf("duplicate sessions should be flagged duplicate: dup1=%+v dup2=%+v", dup1, dup2)
	}
	if dup1.DuplicateGroup != "dup-session" || dup2.DuplicateGroup != "dup-session" {
		t.Fatalf("duplicate group mismatch: dup1=%q dup2=%q", dup1.DuplicateGroup, dup2.DuplicateGroup)
	}
	if len(report.ProposalAdoptionQueue) != 1 {
		t.Fatalf("proposal_adoption_queue len = %d, want 1", len(report.ProposalAdoptionQueue))
	}
	if report.ProposalAdoptionQueue[0].ProposalSnapshot != "myassistant-create-repo-contract" {
		t.Fatalf("proposal_snapshot_id = %q", report.ProposalAdoptionQueue[0].ProposalSnapshot)
	}
	if len(report.AgentTasks) != 1 {
		t.Fatalf("agent_tasks len = %d, want 1", len(report.AgentTasks))
	}
	if report.AgentTasks[0].Name != "myassistant-create-repo-contract-adoption-1" {
		t.Fatalf("agent task name = %q", report.AgentTasks[0].Name)
	}
	if len(report.AgentTaskDecisions) != 1 {
		t.Fatalf("agent_task_decisions len = %d, want 1", len(report.AgentTaskDecisions))
	}
	if report.AgentTaskDecisions[0].SelectedAgent != "codex" {
		t.Fatalf("selected_agent = %q", report.AgentTaskDecisions[0].SelectedAgent)
	}
	if len(report.AgentTaskAttempts) != 1 {
		t.Fatalf("agent_task_attempts len = %d, want 1", len(report.AgentTaskAttempts))
	}
	if report.AgentTaskAttempts[0].SessionName != "myassistant-myassistant-create-repo-contract" {
		t.Fatalf("attempt session_name = %q", report.AgentTaskAttempts[0].SessionName)
	}
	if len(report.AgentTaskOutcomes) != 1 {
		t.Fatalf("agent_task_outcomes len = %d, want 1", len(report.AgentTaskOutcomes))
	}
	if report.AgentTaskOutcomes[0].PRURL != "https://github.com/chaspy/myassistant/pull/12" {
		t.Fatalf("outcome pr_url = %q", report.AgentTaskOutcomes[0].PRURL)
	}
}

func TestBuildStateShowReportIncludesRecentActionMetadata(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := store.LogAction(db, &store.Action{
		SessionID:      "codex:a/b:s1",
		ActionType:     "handoff",
		Content:        "worker session handoff recorded",
		RouteReason:    "research task prefers codex",
		HandoffSummary: "captured comparison notes and left follow-up",
		TokenBurn:      2048,
	}); err != nil {
		t.Fatalf("LogAction: %v", err)
	}

	report, err := buildStateShowReport(db)
	if err != nil {
		t.Fatalf("buildStateShowReport: %v", err)
	}
	if len(report.RecentActions) != 1 {
		t.Fatalf("recent actions = %d, want 1", len(report.RecentActions))
	}

	action := report.RecentActions[0]
	if action.RouteReason != "research task prefers codex" {
		t.Fatalf("route_reason = %q", action.RouteReason)
	}
	if action.HandoffSummary != "captured comparison notes and left follow-up" {
		t.Fatalf("handoff_summary = %q", action.HandoffSummary)
	}
	if action.TokenBurn != 2048 {
		t.Fatalf("token_burn = %d", action.TokenBurn)
	}
}

func TestBuildStateShowReportIncludesQueuedAdoptions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := store.CreateSessionAdoption(db, &store.SessionAdoption{
		Agent:                 "codex",
		Mux:                   "zellij",
		ZellijSession:         "atama-codex-1733",
		Repository:            "chaspy/agentctl",
		CWD:                   "/tmp/atama-agentctl",
		GitBranch:             "feat/protected-codex-adopt",
		Strategy:              store.AdoptionStrategyProtected,
		TargetPermissionLevel: store.PermissionSuggest,
		Note:                  "protect before apply",
	}); err != nil {
		t.Fatalf("CreateSessionAdoption: %v", err)
	}

	report, err := buildStateShowReport(db)
	if err != nil {
		t.Fatalf("buildStateShowReport: %v", err)
	}
	if report.Summary.QueuedAdoptions != 1 {
		t.Fatalf("queued_adoptions = %d, want 1", report.Summary.QueuedAdoptions)
	}
	if len(report.AdoptionQueue) != 1 {
		t.Fatalf("adoption_queue len = %d, want 1", len(report.AdoptionQueue))
	}

	adoption := report.AdoptionQueue[0]
	if adoption.ZellijSession != "atama-codex-1733" {
		t.Fatalf("zellij_session = %q", adoption.ZellijSession)
	}
	if adoption.TargetPermission != "1 (suggest)" {
		t.Fatalf("target_permission = %q", adoption.TargetPermission)
	}
}
