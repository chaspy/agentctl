package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/store"
)

func TestHandleSessionsIncludesExtendedSessionFields(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	lastActive := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:abc123",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "abc123",
		CWD:           "/tmp/myassistant",
		GitBranch:     "feat/chat-api",
		ZellijSession: "chat-api-codex",
		Status:        "blocked",
		BlockedReason: "input required",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastMessage:   "needs review",
		LastActive:    lastActive,
		TaskSummary:   "chat backend",
		Role:          "worker",
		IsLoop:        true,
		IsProtected:   true,
		PRNumber:      150,
		PRURL:         "https://github.com/chaspy/myassistant/pull/150",
		PRState:       "OPEN",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		ID            string `json:"id"`
		SessionID     string `json:"session_id"`
		CWD           string `json:"cwd"`
		ZellijSession string `json:"zellij_session"`
		BlockedReason string `json:"blocked_reason"`
		IsLoop        bool   `json:"is_loop"`
		IsProtected   bool   `json:"is_protected"`
		PRURL         string `json:"pr_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].ID != "codex:chaspy/myassistant:abc123" {
		t.Fatalf("id = %q", payload[0].ID)
	}
	if payload[0].SessionID != "abc123" {
		t.Fatalf("session_id = %q", payload[0].SessionID)
	}
	if payload[0].CWD != "/tmp/myassistant" {
		t.Fatalf("cwd = %q", payload[0].CWD)
	}
	if payload[0].ZellijSession != "chat-api-codex" {
		t.Fatalf("zellij_session = %q", payload[0].ZellijSession)
	}
	if payload[0].BlockedReason != "input required" {
		t.Fatalf("blocked_reason = %q", payload[0].BlockedReason)
	}
	if !payload[0].IsLoop {
		t.Fatalf("is_loop = false, want true")
	}
	if !payload[0].IsProtected {
		t.Fatalf("is_protected = false, want true")
	}
	if payload[0].PRURL != "https://github.com/chaspy/myassistant/pull/150" {
		t.Fatalf("pr_url = %q", payload[0].PRURL)
	}
}

func TestHandleSessionDetailResolvesBySessionKeys(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:def456",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "provider-session-1",
		CWD:           "/tmp/myassistant",
		GitBranch:     "feat/session-detail",
		ZellijSession: "session-detail-codex",
		Status:        "idle",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastMessage:   "done",
		LastActive:    time.Date(2026, 4, 25, 12, 30, 0, 0, time.UTC),
		Role:          "worker",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	for _, key := range []string{"codex:chaspy/myassistant:def456", "provider-session-1", "session-detail-codex"} {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/detail?key="+key, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("key=%q status = %d, body = %s", key, rec.Code, rec.Body.String())
		}

		var payload struct {
			ID            string `json:"id"`
			SessionID     string `json:"session_id"`
			ZellijSession string `json:"zellij_session"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("key=%q json.Unmarshal: %v", key, err)
		}
		if payload.ID != "codex:chaspy/myassistant:def456" {
			t.Fatalf("key=%q id = %q", key, payload.ID)
		}
		if payload.SessionID != "provider-session-1" {
			t.Fatalf("key=%q session_id = %q", key, payload.SessionID)
		}
		if payload.ZellijSession != "session-detail-codex" {
			t.Fatalf("key=%q zellij_session = %q", key, payload.ZellijSession)
		}
	}
}

func TestHandleSessionSummarySupportsSessionKey(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:summary-1",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "summary-session-1",
		CWD:           "/tmp/myassistant",
		GitBranch:     "feat/summary-api",
		ZellijSession: "summary-api-codex",
		Status:        "idle",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastActive:    time.Date(2026, 4, 25, 13, 0, 0, 0, time.UTC),
		Role:          "worker",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	body := bytes.NewBufferString(`{"session_key":"summary-session-1","summary":"updated summary"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/summary", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	got, err := store.GetSession(db, "codex:chaspy/myassistant:summary-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TaskSummary != "updated summary" {
		t.Fatalf("task_summary = %q, want updated summary", got.TaskSummary)
	}
}

func TestHandleTasksIncludesOwnerAndPRURL(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.CreateTask(db, &store.Task{
		SessionID:   "summary-session-1",
		Description: "Review PR #150",
		Status:      "pending",
		Owner:       "chaspy",
		PRURL:       "https://github.com/chaspy/myassistant/pull/150",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		Owner string `json:"owner"`
		PRURL string `json:"pr_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].Owner != "chaspy" {
		t.Fatalf("owner = %q, want chaspy", payload[0].Owner)
	}
	if payload[0].PRURL != "https://github.com/chaspy/myassistant/pull/150" {
		t.Fatalf("pr_url = %q", payload[0].PRURL)
	}
}

func TestHandleSessionMarkDeadUpdatesSession(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertSession(db, &store.Session{
		ID:            "codex:chaspy/myassistant:dead-1",
		Agent:         "codex",
		Repository:    "chaspy/myassistant",
		SessionID:     "dead-session-1",
		CWD:           "/tmp/myassistant",
		GitBranch:     "feat/dead-api",
		ZellijSession: "dead-api-codex",
		Status:        "idle",
		DesiredState:  store.DesiredStateRunning,
		RuntimeStatus: "running",
		LastActive:    time.Date(2026, 4, 25, 13, 30, 0, 0, time.UTC),
		Role:          "worker",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	body := bytes.NewBufferString(`{"zellij_session":"dead-api-codex"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/mark-dead", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	got, err := store.GetSession(db, "codex:chaspy/myassistant:dead-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Status != "dead" {
		t.Fatalf("status = %q, want dead", got.Status)
	}
	if got.DesiredState != store.DesiredStateStopped {
		t.Fatalf("desired_state = %q, want %q", got.DesiredState, store.DesiredStateStopped)
	}
	if got.RuntimeStatus != "gone" {
		t.Fatalf("runtime_status = %q, want gone", got.RuntimeStatus)
	}
}

func TestHandleReconcileReturnsObservedManagedRepoState(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home")
	repoPath := filepath.Join(home, "go", "src", "github.com", "chaspy", "myassistant")
	contractPath := filepath.Join(repoPath, ".agent", "repo.yaml")
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(contractPath, []byte("kind: RepoContract\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:             "myassistant",
		Repository:       "chaspy/myassistant",
		Role:             "personal-ops-console",
		Visibility:       "private",
		RepoContractPath: ".agent/repo.yaml",
		RawSpecJSON:      `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private","repoContractPath":".agent/repo.yaml"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/reconcile", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Summary struct {
			ManagedRepos       int `json:"managedRepos"`
			LocalClonesFound   int `json:"localClonesFound"`
			RepoContractsFound int `json:"repoContractsFound"`
			TaskProposals      int `json:"taskProposals"`
			NeedsAttention     int `json:"needsAttention"`
		} `json:"summary"`
		Repos []struct {
			Name            string `json:"name"`
			LocalCloneFound bool   `json:"localCloneFound"`
			HasRepoContract bool   `json:"hasRepoContract"`
			NeedsAttention  bool   `json:"needsAttention"`
		} `json:"repos"`
		Proposals []struct {
			ID string `json:"id"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if payload.Summary.ManagedRepos != 1 {
		t.Fatalf("managedRepos = %d, want 1", payload.Summary.ManagedRepos)
	}
	if payload.Summary.LocalClonesFound != 1 {
		t.Fatalf("localClonesFound = %d, want 1", payload.Summary.LocalClonesFound)
	}
	if payload.Summary.RepoContractsFound != 1 {
		t.Fatalf("repoContractsFound = %d, want 1", payload.Summary.RepoContractsFound)
	}
	if payload.Summary.TaskProposals != 0 {
		t.Fatalf("taskProposals = %d, want 0", payload.Summary.TaskProposals)
	}
	if payload.Summary.NeedsAttention != 0 {
		t.Fatalf("needsAttention = %d, want 0", payload.Summary.NeedsAttention)
	}
	if len(payload.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(payload.Repos))
	}
	if payload.Repos[0].Name != "myassistant" {
		t.Fatalf("name = %q, want myassistant", payload.Repos[0].Name)
	}
	if !payload.Repos[0].LocalCloneFound || !payload.Repos[0].HasRepoContract {
		t.Fatalf("unexpected observed state: %+v", payload.Repos[0])
	}
	if payload.Repos[0].NeedsAttention {
		t.Fatalf("expected no attention: %+v", payload.Repos[0])
	}
	if len(payload.Proposals) != 0 {
		t.Fatalf("expected no proposals: %+v", payload.Proposals)
	}
}

func TestHandleProposalAdoptionsReturnsQueuedItems(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.CreateTaskProposalAdoption(db, &store.TaskProposalAdoption{
		ProposalSnapshotID: "agentctl-create-repo-contract",
		Source:             "reconcile",
		ReportMode:         "read-only",
		RepoRef:            "agentctl",
		Repository:         "chaspy/agentctl",
		Category:           "create_repo_contract",
		Title:              "Create repo contract",
		Objective:          "Add .agent/repo.yaml",
		TaskType:           "docs",
		Risk:               "low",
		ApprovalStatus:     "not_required",
		Status:             store.TaskProposalAdoptionStatusQueued,
		OperatorNote:       "carry forward",
	}); err != nil {
		t.Fatalf("CreateTaskProposalAdoption: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/proposal-adoptions", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		ProposalSnapshotID string `json:"proposalSnapshotId"`
		Status             string `json:"status"`
		OperatorNote       string `json:"operatorNote"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].ProposalSnapshotID != "agentctl-create-repo-contract" {
		t.Fatalf("proposalSnapshotId = %q", payload[0].ProposalSnapshotID)
	}
	if payload[0].Status != "queued" {
		t.Fatalf("status = %q, want queued", payload[0].Status)
	}
	if payload[0].OperatorNote != "carry forward" {
		t.Fatalf("operatorNote = %q", payload[0].OperatorNote)
	}
}

func TestHandleAgentTasksReturnsMaterializedItems(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

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

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/agent-tasks", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		Name       string `json:"name"`
		SourceKind string `json:"sourceKind"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].Name != "agentctl-create-repo-contract-adoption-1" {
		t.Fatalf("name = %q", payload[0].Name)
	}
	if payload[0].SourceKind != "proposal_adoption" {
		t.Fatalf("sourceKind = %q", payload[0].SourceKind)
	}
	if payload[0].Status != "planned" {
		t.Fatalf("status = %q", payload[0].Status)
	}
}

func TestHandleAgentTaskDecisionsReturnsRecordedItems(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.CreateAgentTaskDecision(db, &store.AgentTaskDecision{
		AgentTaskName:     "agentctl-create-repo-contract-adoption-1",
		RepoRef:           "agentctl",
		Repository:        "chaspy/agentctl",
		TaskType:          "docs",
		Risk:              "low",
		RoutingPolicyRef:  "control-plane-default",
		PolicyVersion:     "unresolved",
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

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/agent-task-decisions", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		AgentTaskName    string   `json:"agentTaskName"`
		SelectedAgent    string   `json:"selectedAgent"`
		EligibleAgents   []string `json:"eligibleAgents"`
		RoutingPolicyRef string   `json:"routingPolicyRef"`
		Status           string   `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].AgentTaskName != "agentctl-create-repo-contract-adoption-1" {
		t.Fatalf("agentTaskName = %q", payload[0].AgentTaskName)
	}
	if payload[0].SelectedAgent != "codex" {
		t.Fatalf("selectedAgent = %q", payload[0].SelectedAgent)
	}
	if payload[0].RoutingPolicyRef != "control-plane-default" {
		t.Fatalf("routingPolicyRef = %q", payload[0].RoutingPolicyRef)
	}
	if payload[0].Status != "recorded" {
		t.Fatalf("status = %q", payload[0].Status)
	}
	if len(payload[0].EligibleAgents) != 2 {
		t.Fatalf("eligibleAgents len = %d, want 2", len(payload[0].EligibleAgents))
	}
}

func TestHandleAgentTaskAttemptsReturnsSpawnedItems(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

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

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/agent-task-attempts", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		DecisionID       int64  `json:"decisionId"`
		AgentTaskName    string `json:"agentTaskName"`
		SessionName      string `json:"sessionName"`
		ManagedSessionID string `json:"managedSessionId"`
		Status           string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].DecisionID != 1 {
		t.Fatalf("decisionId = %d", payload[0].DecisionID)
	}
	if payload[0].AgentTaskName != "agentctl-create-repo-contract" {
		t.Fatalf("agentTaskName = %q", payload[0].AgentTaskName)
	}
	if payload[0].SessionName != "agentctl-agentctl-create-repo-contract" {
		t.Fatalf("sessionName = %q", payload[0].SessionName)
	}
	if payload[0].ManagedSessionID == "" {
		t.Fatal("expected managedSessionId")
	}
	if payload[0].Status != "spawned" {
		t.Fatalf("status = %q", payload[0].Status)
	}
}

func TestHandleAgentTaskOutcomesReturnsCompletedItems(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.CreateAgentTaskOutcome(db, &store.AgentTaskOutcome{
		AttemptID:        1,
		DecisionID:       2,
		AgentTaskName:    "agentctl-create-repo-contract",
		RepoRef:          "agentctl",
		Repository:       "chaspy/agentctl",
		TaskType:         "docs",
		Risk:             "low",
		Agent:            "codex",
		SessionName:      "agentctl-agentctl-create-repo-contract",
		ManagedSessionID: "codex:chaspy/agentctl:zellij-agentctl-agentctl-create-repo-contract",
		Status:           "completed",
		PRURL:            "https://github.com/chaspy/agentctl/pull/42",
		Source:           "session",
	}); err != nil {
		t.Fatalf("CreateAgentTaskOutcome: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/agent-task-outcomes", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		AttemptID     int64  `json:"attemptId"`
		AgentTaskName string `json:"agentTaskName"`
		Status        string `json:"status"`
		PRURL         string `json:"prUrl"`
		Source        string `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0].AttemptID != 1 {
		t.Fatalf("attemptId = %d", payload[0].AttemptID)
	}
	if payload[0].AgentTaskName != "agentctl-create-repo-contract" {
		t.Fatalf("agentTaskName = %q", payload[0].AgentTaskName)
	}
	if payload[0].Status != "completed" {
		t.Fatalf("status = %q", payload[0].Status)
	}
	if payload[0].PRURL != "https://github.com/chaspy/agentctl/pull/42" {
		t.Fatalf("prUrl = %q", payload[0].PRURL)
	}
	if payload[0].Source != "session" {
		t.Fatalf("source = %q", payload[0].Source)
	}
}
