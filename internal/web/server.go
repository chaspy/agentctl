package web

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/controlplane"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

// SyncFunc is the function signature for syncing sessions to DB.
// It takes (db, agentFilter, hours, regenerateSummaries) and returns (count, error).
type SyncFunc func(*sql.DB, string, int, bool) (int, error)

// Server serves the PWA dashboard and API endpoints.
type Server struct {
	db       *sql.DB
	syncFunc SyncFunc
}

// New creates a new Server with the given database connection and sync function.
func New(db *sql.DB, syncFunc SyncFunc) *Server {
	return &Server{db: db, syncFunc: syncFunc}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/detail", s.handleSessionDetail)
	mux.HandleFunc("/api/sessions/mark-dead", s.handleSessionMarkDead)
	mux.HandleFunc("/api/sessions/summary", s.handleSessionSummary)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/actions", s.handleActions)
	mux.HandleFunc("/api/rate", s.handleRate)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/reconcile", s.handleReconcile)
	mux.HandleFunc("/api/control-plane-resources", s.handleControlPlaneResources)
	mux.HandleFunc("/api/proposals", s.handleProposals)
	mux.HandleFunc("/api/proposal-adoptions", s.handleProposalAdoptions)
	mux.HandleFunc("/api/agent-tasks", s.handleAgentTasks)
	mux.HandleFunc("/api/agent-task-decisions", s.handleAgentTaskDecisions)
	mux.HandleFunc("/api/agent-task-attempts", s.handleAgentTaskAttempts)
	mux.HandleFunc("/api/agent-task-outcomes", s.handleAgentTaskOutcomes)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/resume", s.handleResume)
	mux.HandleFunc("/api/sessions/messages", s.handleSessionMessages)

	// Static files (PWA)
	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	return mux
}

type sessionJSON struct {
	ID              string `json:"id"`
	Agent           string `json:"agent"`
	Repository      string `json:"repository"`
	SessionID       string `json:"session_id"`
	CWD             string `json:"cwd"`
	GitBranch       string `json:"git_branch"`
	ZellijSession   string `json:"zellij_session"`
	Status          string `json:"status"`
	BlockedReason   string `json:"blocked_reason,omitempty"`
	DesiredState    string `json:"desired_state"`
	RuntimeStatus   string `json:"runtime_status"`
	Alive           bool   `json:"alive"`
	LastMessage     string `json:"last_message"`
	LastActive      string `json:"last_active"`
	LastSentAt      string `json:"last_sent_at"`
	LastMessageAt   string `json:"last_message_at"`
	LastSeenAliveAt string `json:"last_seen_alive_at"`
	LastObservedAt  string `json:"last_observed_at"`
	TaskSummary     string `json:"task_summary"`
	Role            string `json:"role"`
	Archived        bool   `json:"archived"`
	IsLoop          bool   `json:"is_loop"`
	IsProtected     bool   `json:"is_protected"`
	PRNumber        int    `json:"pr_number,omitempty"`
	PRURL           string `json:"pr_url,omitempty"`
	PRState         string `json:"pr_state,omitempty"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	showAll := r.URL.Query().Get("all") == "true"

	var sessions []store.Session
	var err error
	if showAll {
		sessions, err = store.ListAllSessionsWithArchive(s.db)
	} else {
		sessions, err = store.ListActiveSessions(s.db)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]sessionJSON, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sessionToJSON(sess))
	}
	writeJSON(w, out)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	detail, err := s.findSessionDetail(key)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessionToJSON(*detail))
}

func (s *Server) findSessionDetail(key string) (*store.Session, error) {
	if detail, err := store.GetSessionAny(s.db, key); err == nil {
		return detail, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	if detail, err := store.GetSessionBySessionID(s.db, key); err == nil {
		return detail, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	return store.GetSessionByZellijSession(s.db, key)
}

func sessionToJSON(sess store.Session) sessionJSON {
	return sessionJSON{
		ID:              sess.ID,
		Agent:           sess.Agent,
		Repository:      sess.Repository,
		SessionID:       sess.SessionID,
		CWD:             sess.CWD,
		GitBranch:       sess.GitBranch,
		ZellijSession:   sess.ZellijSession,
		Status:          sess.Status,
		BlockedReason:   sess.BlockedReason,
		DesiredState:    sess.DesiredState,
		RuntimeStatus:   sess.RuntimeStatus,
		Alive:           sess.WantsRunning(),
		LastMessage:     sess.LastMessage,
		LastActive:      sess.LastActive.Format(time.RFC3339),
		LastSentAt:      formatOptionalRFC3339(sess.LastSentAt),
		LastMessageAt:   formatOptionalRFC3339(sess.LastMessageAt),
		LastSeenAliveAt: formatOptionalRFC3339(sess.LastSeenAliveAt),
		LastObservedAt:  formatOptionalRFC3339(sess.ObservedActivityAt()),
		TaskSummary:     sess.TaskSummary,
		Role:            sess.Role,
		Archived:        sess.Archived,
		IsLoop:          sess.IsLoop,
		IsProtected:     sess.IsProtected,
		PRNumber:        sess.PRNumber,
		PRURL:           sess.PRURL,
		PRState:         sess.PRState,
	}
}

func formatOptionalRFC3339(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}

type summaryRequest struct {
	ID         string `json:"id"`
	SessionKey string `json:"session_key,omitempty"`
	Summary    string `json:"summary"`
}

func (s *Server) handleSessionSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req summaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.ID == "" && req.SessionKey == "") {
		http.Error(w, "id or session_key and summary required", http.StatusBadRequest)
		return
	}

	sessionID := req.ID
	if sessionID == "" {
		detail, err := s.findSessionDetail(strings.TrimSpace(req.SessionKey))
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sessionID = detail.ID
	}

	_, err := s.db.Exec("UPDATE sessions SET task_summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", req.Summary, sessionID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]string{"ok": "updated"})
}

type taskJSON struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Owner       string `json:"owner,omitempty"`
	AssignedAt  string `json:"assigned_at"`
	Result      string `json:"result,omitempty"`
	PRURL       string `json:"pr_url,omitempty"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := store.GetActiveTasks(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskJSON{
			ID:          t.ID,
			SessionID:   t.SessionID,
			Description: t.Description,
			Status:      t.Status,
			Owner:       t.Owner,
			AssignedAt:  t.AssignedAt.Format(time.RFC3339),
			Result:      t.Result,
			PRURL:       t.PRURL,
		})
	}
	writeJSON(w, out)
}

type sessionMarkDeadRequest struct {
	ZellijSession string `json:"zellij_session"`
}

func (s *Server) handleSessionMarkDead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req sessionMarkDeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ZellijSession) == "" {
		http.Error(w, "zellij_session required", http.StatusBadRequest)
		return
	}

	rows, err := store.MarkSessionDeadByZellijSession(s.db, strings.TrimSpace(req.ZellijSession))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"ok":           true,
		"updated_rows": rows,
	})
}

type actionJSON struct {
	ID             int64  `json:"id"`
	SessionID      string `json:"session_id"`
	ActionType     string `json:"action_type"`
	Content        string `json:"content"`
	Result         string `json:"result,omitempty"`
	RouteReason    string `json:"route_reason,omitempty"`
	HandoffSummary string `json:"handoff_summary,omitempty"`
	TokenBurn      int    `json:"token_burn,omitempty"`
	CreatedAt      string `json:"created_at"`
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	actions, err := store.GetRecentActions(s.db, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]actionJSON, 0, len(actions))
	for _, a := range actions {
		out = append(out, actionJSON{
			ID:             a.ID,
			SessionID:      a.SessionID,
			ActionType:     a.ActionType,
			Content:        a.Content,
			Result:         a.Result,
			RouteReason:    a.RouteReason,
			HandoffSummary: a.HandoffSummary,
			TokenBurn:      a.TokenBurn,
			CreatedAt:      a.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, out)
}

type rateJSON struct {
	Agent   string `json:"agent"`
	Percent int    `json:"percent"` // remaining capacity (0-100), -1 = unknown
	Detail  string `json:"detail"`
	Resets  string `json:"resets,omitempty"` // reset time like "02:00"
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	var rates []rateJSON

	claudeRate, err := provider.ClaudeRate()
	if err == nil {
		rates = append(rates, buildRateJSON("claude", claudeRate))
	}

	codexRate, err := provider.CodexRate()
	if err == nil {
		rates = append(rates, buildRateJSON("codex", codexRate))
	}

	writeJSON(w, rates)
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	report, err := controlplane.BuildReconcileReport(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, report)
}

type controlPlaneResourceJSON struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	APIVersion   string `json:"apiVersion,omitempty"`
	SourcePath   string `json:"sourcePath,omitempty"`
	SourceCommit string `json:"sourceCommit,omitempty"`
	SpecHash     string `json:"specHash,omitempty"`
	Spec         any    `json:"spec,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

func (s *Server) handleControlPlaneResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	resources, err := store.ListControlPlaneResources(s.db, kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]controlPlaneResourceJSON, 0, len(resources))
	for _, resource := range resources {
		out = append(out, controlPlaneResourceToJSON(resource))
	}
	writeJSON(w, out)
}

func controlPlaneResourceToJSON(resource store.ControlPlaneResource) controlPlaneResourceJSON {
	payload := controlPlaneResourceJSON{
		Kind:         resource.Kind,
		Name:         resource.Name,
		APIVersion:   resource.APIVersion,
		SourcePath:   resource.SourcePath,
		SourceCommit: resource.SourceCommit,
		SpecHash:     resource.SpecHash,
		CreatedAt:    resource.CreatedAt,
		UpdatedAt:    resource.UpdatedAt,
	}
	if resource.RawSpecJSON != "" {
		var spec any
		if err := json.Unmarshal([]byte(resource.RawSpecJSON), &spec); err == nil {
			payload.Spec = spec
		}
	}
	return payload
}

type proposalJSON struct {
	ID                string   `json:"id"`
	Source            string   `json:"source"`
	ReportMode        string   `json:"reportMode"`
	RepoRef           string   `json:"repoRef"`
	Repository        string   `json:"repository"`
	Tier              string   `json:"tier,omitempty"`
	Category          string   `json:"category"`
	Title             string   `json:"title"`
	Objective         string   `json:"objective"`
	TaskType          string   `json:"taskType"`
	Risk              string   `json:"risk"`
	ReviewPolicyRef   string   `json:"reviewPolicyRef,omitempty"`
	ApprovalPolicyRef string   `json:"approvalPolicyRef,omitempty"`
	ApprovalStatus    string   `json:"approvalStatus"`
	ApprovalReason    string   `json:"approvalReason,omitempty"`
	DesiredOutcome    []string `json:"desiredOutcome,omitempty"`
	TriggerIssues     []string `json:"triggerIssues,omitempty"`
	UpdatedAt         string   `json:"updatedAt,omitempty"`
}

type proposalAdoptionJSON struct {
	ID                 int64    `json:"id"`
	ProposalSnapshotID string   `json:"proposalSnapshotId"`
	Source             string   `json:"source"`
	ReportMode         string   `json:"reportMode"`
	RepoRef            string   `json:"repoRef"`
	Repository         string   `json:"repository"`
	Tier               string   `json:"tier,omitempty"`
	Category           string   `json:"category"`
	Title              string   `json:"title"`
	Objective          string   `json:"objective"`
	TaskType           string   `json:"taskType"`
	Risk               string   `json:"risk"`
	ReviewPolicyRef    string   `json:"reviewPolicyRef,omitempty"`
	ApprovalPolicyRef  string   `json:"approvalPolicyRef,omitempty"`
	ApprovalStatus     string   `json:"approvalStatus"`
	ApprovalReason     string   `json:"approvalReason,omitempty"`
	DesiredOutcome     []string `json:"desiredOutcome,omitempty"`
	TriggerIssues      []string `json:"triggerIssues,omitempty"`
	Status             string   `json:"status"`
	OperatorNote       string   `json:"operatorNote,omitempty"`
	CreatedAt          string   `json:"createdAt,omitempty"`
	UpdatedAt          string   `json:"updatedAt,omitempty"`
}

type agentTaskJSON struct {
	Name                        string   `json:"name"`
	RepoRef                     string   `json:"repoRef"`
	Repository                  string   `json:"repository"`
	Objective                   string   `json:"objective"`
	TaskType                    string   `json:"taskType"`
	Risk                        string   `json:"risk"`
	ContextRefs                 []string `json:"contextRefs,omitempty"`
	DesiredOutcome              []string `json:"desiredOutcome,omitempty"`
	RoutingPolicyRef            string   `json:"routingPolicyRef,omitempty"`
	ReviewPolicyRef             string   `json:"reviewPolicyRef,omitempty"`
	ApprovalPolicyRef           string   `json:"approvalPolicyRef,omitempty"`
	ApprovalRequiredBeforeMerge bool     `json:"approvalRequiredBeforeMerge"`
	SourceKind                  string   `json:"sourceKind"`
	SourceRef                   string   `json:"sourceRef,omitempty"`
	Status                      string   `json:"status"`
	CreatedAt                   string   `json:"createdAt,omitempty"`
	UpdatedAt                   string   `json:"updatedAt,omitempty"`
}

type agentTaskDecisionJSON struct {
	ID                int64    `json:"id"`
	AgentTaskName     string   `json:"agentTaskName"`
	RepoRef           string   `json:"repoRef"`
	Repository        string   `json:"repository"`
	TaskType          string   `json:"taskType"`
	Risk              string   `json:"risk"`
	RoutingPolicyRef  string   `json:"routingPolicyRef,omitempty"`
	PolicyVersion     string   `json:"policyVersion,omitempty"`
	SelectionMode     string   `json:"selectionMode,omitempty"`
	SelectedAgent     string   `json:"selectedAgent"`
	SelectedRepoMode  string   `json:"selectedRepoMode,omitempty"`
	RepoProfileSource string   `json:"repoProfileSource,omitempty"`
	ModeSource        string   `json:"modeSource,omitempty"`
	AgentSource       string   `json:"agentSource,omitempty"`
	EligibleAgents    []string `json:"eligibleAgents,omitempty"`
	RouteReason       string   `json:"routeReason,omitempty"`
	Status            string   `json:"status"`
	CreatedAt         string   `json:"createdAt,omitempty"`
	UpdatedAt         string   `json:"updatedAt,omitempty"`
}

type agentTaskAttemptJSON struct {
	ID               int64  `json:"id"`
	DecisionID       int64  `json:"decisionId"`
	AgentTaskName    string `json:"agentTaskName"`
	RepoRef          string `json:"repoRef"`
	Repository       string `json:"repository"`
	TaskType         string `json:"taskType"`
	Risk             string `json:"risk"`
	Agent            string `json:"agent"`
	RepoMode         string `json:"repoMode,omitempty"`
	Branch           string `json:"branch,omitempty"`
	SessionName      string `json:"sessionName,omitempty"`
	ManagedSessionID string `json:"managedSessionId,omitempty"`
	WorkDir          string `json:"workDir,omitempty"`
	LaunchCommand    string `json:"launchCommand,omitempty"`
	InitialMessage   string `json:"initialMessage,omitempty"`
	Summary          string `json:"summary,omitempty"`
	Status           string `json:"status"`
	FailureReason    string `json:"failureReason,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

type agentTaskOutcomeJSON struct {
	ID               int64  `json:"id"`
	AttemptID        int64  `json:"attemptId"`
	DecisionID       int64  `json:"decisionId"`
	AgentTaskName    string `json:"agentTaskName"`
	RepoRef          string `json:"repoRef"`
	Repository       string `json:"repository"`
	TaskType         string `json:"taskType"`
	Risk             string `json:"risk"`
	Agent            string `json:"agent"`
	Branch           string `json:"branch,omitempty"`
	SessionName      string `json:"sessionName,omitempty"`
	ManagedSessionID string `json:"managedSessionId,omitempty"`
	Status           string `json:"status"`
	ResultSummary    string `json:"resultSummary,omitempty"`
	PRNumber         int    `json:"prNumber,omitempty"`
	PRURL            string `json:"prUrl,omitempty"`
	PRState          string `json:"prState,omitempty"`
	CommitSHA        string `json:"commitSha,omitempty"`
	FailureCategory  string `json:"failureCategory,omitempty"`
	FailureReason    string `json:"failureReason,omitempty"`
	Source           string `json:"source,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	proposals, err := store.ListTaskProposalSnapshots(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]proposalJSON, 0, len(proposals))
	for _, proposal := range proposals {
		out = append(out, proposalJSON{
			ID:                proposal.ID,
			Source:            proposal.Source,
			ReportMode:        proposal.ReportMode,
			RepoRef:           proposal.RepoRef,
			Repository:        proposal.Repository,
			Tier:              proposal.Tier,
			Category:          proposal.Category,
			Title:             proposal.Title,
			Objective:         proposal.Objective,
			TaskType:          proposal.TaskType,
			Risk:              proposal.Risk,
			ReviewPolicyRef:   proposal.ReviewPolicyRef,
			ApprovalPolicyRef: proposal.ApprovalPolicyRef,
			ApprovalStatus:    proposal.ApprovalStatus,
			ApprovalReason:    proposal.ApprovalReason,
			DesiredOutcome:    proposal.DesiredOutcome,
			TriggerIssues:     proposal.TriggerIssues,
			UpdatedAt:         proposal.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleProposalAdoptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	adoptions, err := store.ListTaskProposalAdoptions(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]proposalAdoptionJSON, 0, len(adoptions))
	for _, adoption := range adoptions {
		out = append(out, proposalAdoptionJSON{
			ID:                 adoption.ID,
			ProposalSnapshotID: adoption.ProposalSnapshotID,
			Source:             adoption.Source,
			ReportMode:         adoption.ReportMode,
			RepoRef:            adoption.RepoRef,
			Repository:         adoption.Repository,
			Tier:               adoption.Tier,
			Category:           adoption.Category,
			Title:              adoption.Title,
			Objective:          adoption.Objective,
			TaskType:           adoption.TaskType,
			Risk:               adoption.Risk,
			ReviewPolicyRef:    adoption.ReviewPolicyRef,
			ApprovalPolicyRef:  adoption.ApprovalPolicyRef,
			ApprovalStatus:     adoption.ApprovalStatus,
			ApprovalReason:     adoption.ApprovalReason,
			DesiredOutcome:     adoption.DesiredOutcome,
			TriggerIssues:      adoption.TriggerIssues,
			Status:             adoption.Status,
			OperatorNote:       adoption.OperatorNote,
			CreatedAt:          adoption.CreatedAt,
			UpdatedAt:          adoption.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleAgentTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	tasks, err := store.ListAgentTasks(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]agentTaskJSON, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, agentTaskJSON{
			Name:                        task.Name,
			RepoRef:                     task.RepoRef,
			Repository:                  task.Repository,
			Objective:                   task.Objective,
			TaskType:                    task.TaskType,
			Risk:                        task.Risk,
			ContextRefs:                 task.ContextRefs,
			DesiredOutcome:              task.DesiredOutcome,
			RoutingPolicyRef:            task.RoutingPolicyRef,
			ReviewPolicyRef:             task.ReviewPolicyRef,
			ApprovalPolicyRef:           task.ApprovalPolicyRef,
			ApprovalRequiredBeforeMerge: task.ApprovalRequiredBeforeMerge,
			SourceKind:                  task.SourceKind,
			SourceRef:                   task.SourceRef,
			Status:                      task.Status,
			CreatedAt:                   task.CreatedAt,
			UpdatedAt:                   task.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleAgentTaskDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	decisions, err := store.ListAgentTaskDecisions(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]agentTaskDecisionJSON, 0, len(decisions))
	for _, decision := range decisions {
		out = append(out, agentTaskDecisionJSON{
			ID:                decision.ID,
			AgentTaskName:     decision.AgentTaskName,
			RepoRef:           decision.RepoRef,
			Repository:        decision.Repository,
			TaskType:          decision.TaskType,
			Risk:              decision.Risk,
			RoutingPolicyRef:  decision.RoutingPolicyRef,
			PolicyVersion:     decision.PolicyVersion,
			SelectionMode:     decision.SelectionMode,
			SelectedAgent:     decision.SelectedAgent,
			SelectedRepoMode:  decision.SelectedRepoMode,
			RepoProfileSource: decision.RepoProfileSource,
			ModeSource:        decision.ModeSource,
			AgentSource:       decision.AgentSource,
			EligibleAgents:    decision.EligibleAgents,
			RouteReason:       decision.RouteReason,
			Status:            decision.Status,
			CreatedAt:         decision.CreatedAt,
			UpdatedAt:         decision.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleAgentTaskAttempts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	attempts, err := store.ListAgentTaskAttempts(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]agentTaskAttemptJSON, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, agentTaskAttemptJSON{
			ID:               attempt.ID,
			DecisionID:       attempt.DecisionID,
			AgentTaskName:    attempt.AgentTaskName,
			RepoRef:          attempt.RepoRef,
			Repository:       attempt.Repository,
			TaskType:         attempt.TaskType,
			Risk:             attempt.Risk,
			Agent:            attempt.Agent,
			RepoMode:         attempt.RepoMode,
			Branch:           attempt.Branch,
			SessionName:      attempt.SessionName,
			ManagedSessionID: attempt.ManagedSessionID,
			WorkDir:          attempt.WorkDir,
			LaunchCommand:    attempt.LaunchCommand,
			InitialMessage:   attempt.InitialMessage,
			Summary:          attempt.Summary,
			Status:           attempt.Status,
			FailureReason:    attempt.FailureReason,
			CreatedAt:        attempt.CreatedAt,
			UpdatedAt:        attempt.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleAgentTaskOutcomes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	outcomes, err := store.ListAgentTaskOutcomes(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]agentTaskOutcomeJSON, 0, len(outcomes))
	for _, outcome := range outcomes {
		out = append(out, agentTaskOutcomeJSON{
			ID:               outcome.ID,
			AttemptID:        outcome.AttemptID,
			DecisionID:       outcome.DecisionID,
			AgentTaskName:    outcome.AgentTaskName,
			RepoRef:          outcome.RepoRef,
			Repository:       outcome.Repository,
			TaskType:         outcome.TaskType,
			Risk:             outcome.Risk,
			Agent:            outcome.Agent,
			Branch:           outcome.Branch,
			SessionName:      outcome.SessionName,
			ManagedSessionID: outcome.ManagedSessionID,
			Status:           outcome.Status,
			ResultSummary:    outcome.ResultSummary,
			PRNumber:         outcome.PRNumber,
			PRURL:            outcome.PRURL,
			PRState:          outcome.PRState,
			CommitSHA:        outcome.CommitSHA,
			FailureCategory:  outcome.FailureCategory,
			FailureReason:    outcome.FailureReason,
			Source:           outcome.Source,
			CreatedAt:        outcome.CreatedAt,
			UpdatedAt:        outcome.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

func buildRateJSON(agent string, info provider.RateInfo) rateJSON {
	r := rateJSON{
		Agent:   agent,
		Percent: info.RemainingPct,
	}

	// Build a short, useful detail string
	summary := info.Summary
	if info.RemainingPct >= 0 {
		r.Detail = fmt.Sprintf("%d%% left", info.RemainingPct)
	} else {
		// Use the summary (e.g. "allowed ($18.6 spent, window ends 02:00 ...)")
		// instead of just "unknown"
		r.Detail = summary
	}

	// Extract reset time if present in summary
	if idx := strings.Index(summary, "window ends "); idx >= 0 {
		rest := summary[idx+len("window ends "):]
		if len(rest) >= 5 {
			r.Resets = rest[:5] // "02:00"
		}
	} else if idx := strings.Index(summary, "resets "); idx >= 0 {
		rest := summary[idx+len("resets "):]
		if len(rest) >= 5 {
			r.Resets = rest[:5]
		}
	}

	// Add burn rate from BurnRate field
	if info.BurnRate != "" && info.BurnRate != "-" {
		r.Detail += " | " + info.BurnRate
	}

	return r
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := store.AllState(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, state)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	count, err := s.syncFunc(s.db, "all", 24, false)
	if err != nil {
		http.Error(w, "sync error: "+err.Error(), 500)
		return
	}

	writeJSON(w, map[string]int{"synced": count})
}

type resumeRequest struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	// Find the session in scanned data
	maxAge := 7 * 24 * time.Hour
	sessions, err := provider.ScanClaudeSessions(maxAge)
	if err != nil {
		http.Error(w, "scan error: "+err.Error(), 500)
		return
	}

	var target *provider.SessionInfo
	for i := range sessions {
		if sessions[i].SessionID == req.SessionID {
			target = &sessions[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Build zellij session name
	sessionName := fmt.Sprintf("resume-%s", req.SessionID[:8])

	// Check if zellij session already exists
	existing, _ := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	for _, line := range splitLines(string(existing)) {
		if line == sessionName {
			writeJSON(w, map[string]string{"error": "zellij session already exists", "session": sessionName})
			return
		}
	}

	// Run the resume command in background
	cmd := exec.Command("go", "run", ".", "resume", req.SessionID, "--name", sessionName)
	cmd.Dir = findProjectRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("resume failed: %s\n%s", err, string(out)), 500)
		return
	}

	_ = store.LogAction(s.db, &store.Action{
		SessionID:  sessionName,
		ActionType: "resume",
		Content:    fmt.Sprintf("Resumed session %s via dashboard", req.SessionID),
	})

	writeJSON(w, map[string]string{"ok": "resumed", "session": sessionName})
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	compositeID := r.URL.Query().Get("id")
	if compositeID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	// Extract UUID from composite ID (format: "agent:project:uuid")
	parts := strings.SplitN(compositeID, ":", 3)
	if len(parts) < 3 {
		http.Error(w, "invalid id format", http.StatusBadRequest)
		return
	}
	uuid := parts[2]

	// Scan sessions to find the JSONL file path
	maxAge := 7 * 24 * time.Hour
	sessions, _ := provider.ScanClaudeSessions(maxAge)
	codexSessions, _ := provider.ScanCodexSessions(maxAge)
	sessions = append(sessions, codexSessions...)

	var filePath string
	for _, sess := range sessions {
		if sess.SessionID == uuid {
			filePath = sess.FilePath
			break
		}
	}

	if filePath == "" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	messages, err := session.RecentMessages(filePath, limit)
	if err != nil {
		http.Error(w, "read error: "+err.Error(), 500)
		return
	}

	writeJSON(w, messages)
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range splitByNewline(s) {
		t := trimSpace(line)
		if t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

func findProjectRoot() string {
	// The binary should be running from the project directory
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		return "."
	}
	mod := trimSpace(string(out))
	if mod == "" {
		return "."
	}
	// go.mod path -> directory
	for i := len(mod) - 1; i >= 0; i-- {
		if mod[i] == '/' {
			return mod[:i]
		}
	}
	return "."
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(v)
}
