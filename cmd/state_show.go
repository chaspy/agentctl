package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

type stateShowSummary struct {
	ActiveSessions          int `json:"active_sessions"`
	ArchivedSessions        int `json:"archived_sessions"`
	QueuedAdoptions         int `json:"queued_adoptions"`
	ManagedRepos            int `json:"managed_repos"`
	TaskProposals           int `json:"task_proposals"`
	QueuedProposalAdoptions int `json:"queued_proposal_adoptions"`
	AgentTasks              int `json:"agent_tasks"`
	AgentTaskDecisions      int `json:"agent_task_decisions"`
	AgentTaskAttempts       int `json:"agent_task_attempts"`
	BlockedSessions         int `json:"blocked_sessions"`
	ErrorSessions           int `json:"error_sessions"`
	GhostSessions           int `json:"ghost_sessions"`
	DuplicateSessions       int `json:"duplicate_sessions"`
	DuplicateGroups         int `json:"duplicate_groups"`
	DeadSessions            int `json:"dead_sessions"`
}

type stateShowSession struct {
	ID             string    `json:"id"`
	Agent          string    `json:"agent"`
	Repository     string    `json:"repository"`
	Branch         string    `json:"branch"`
	Status         string    `json:"status"`
	BlockedReason  string    `json:"blocked_reason,omitempty"`
	DesiredState   string    `json:"desired_state"`
	Alive          bool      `json:"alive"`
	RuntimeStatus  string    `json:"runtime_status"`
	ZellijSession  string    `json:"zellij_session,omitempty"`
	TaskSummary    string    `json:"task_summary,omitempty"`
	PRURL          string    `json:"pr_url,omitempty"`
	LastActive     time.Time `json:"last_active"`
	Ghost          bool      `json:"ghost"`
	Duplicate      bool      `json:"duplicate"`
	DuplicateGroup string    `json:"duplicate_group,omitempty"`
	DuplicateCount int       `json:"duplicate_count,omitempty"`
	Health         []string  `json:"health,omitempty"`
}

type stateShowAction struct {
	ID             int64     `json:"id"`
	SessionID      string    `json:"session_id,omitempty"`
	ActionType     string    `json:"action_type"`
	Content        string    `json:"content"`
	Result         string    `json:"result,omitempty"`
	RouteReason    string    `json:"route_reason,omitempty"`
	HandoffSummary string    `json:"handoff_summary,omitempty"`
	TokenBurn      int       `json:"token_burn,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type stateShowAdoption struct {
	ID               int64     `json:"id"`
	Agent            string    `json:"agent"`
	Mux              string    `json:"mux"`
	ZellijSession    string    `json:"zellij_session"`
	ExternalSession  string    `json:"external_session_id,omitempty"`
	Repository       string    `json:"repository,omitempty"`
	Branch           string    `json:"branch,omitempty"`
	CWD              string    `json:"cwd,omitempty"`
	Strategy         string    `json:"strategy"`
	TargetPermission string    `json:"target_permission"`
	Status           string    `json:"status"`
	Note             string    `json:"note,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type stateShowProposalAdoption struct {
	ID               int64    `json:"id"`
	ProposalSnapshot string   `json:"proposal_snapshot_id"`
	RepoRef          string   `json:"repo_ref"`
	Repository       string   `json:"repository"`
	Category         string   `json:"category"`
	TaskType         string   `json:"task_type"`
	Risk             string   `json:"risk"`
	Status           string   `json:"status"`
	ApprovalStatus   string   `json:"approval_status"`
	OperatorNote     string   `json:"operator_note,omitempty"`
	DesiredOutcome   []string `json:"desired_outcome,omitempty"`
	CreatedAt        string   `json:"created_at"`
}

type stateShowAgentTask struct {
	Name        string   `json:"name"`
	RepoRef     string   `json:"repo_ref"`
	Repository  string   `json:"repository"`
	TaskType    string   `json:"task_type"`
	Risk        string   `json:"risk"`
	Status      string   `json:"status"`
	SourceKind  string   `json:"source_kind"`
	ContextRefs []string `json:"context_refs,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

type stateShowAgentTaskDecision struct {
	ID               int64    `json:"id"`
	AgentTaskName    string   `json:"agent_task_name"`
	RepoRef          string   `json:"repo_ref"`
	Repository       string   `json:"repository"`
	TaskType         string   `json:"task_type"`
	Risk             string   `json:"risk"`
	SelectedAgent    string   `json:"selected_agent"`
	SelectedRepoMode string   `json:"selected_repo_mode"`
	Status           string   `json:"status"`
	EligibleAgents   []string `json:"eligible_agents,omitempty"`
	RouteReason      string   `json:"route_reason,omitempty"`
	CreatedAt        string   `json:"created_at"`
}

type stateShowAgentTaskAttempt struct {
	ID               int64  `json:"id"`
	DecisionID       int64  `json:"decision_id"`
	AgentTaskName    string `json:"agent_task_name"`
	RepoRef          string `json:"repo_ref"`
	Repository       string `json:"repository"`
	TaskType         string `json:"task_type"`
	Risk             string `json:"risk"`
	Agent            string `json:"agent"`
	RepoMode         string `json:"repo_mode"`
	Branch           string `json:"branch"`
	SessionName      string `json:"session_name"`
	ManagedSessionID string `json:"managed_session_id"`
	Status           string `json:"status"`
	FailureReason    string `json:"failure_reason,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type stateShowReport struct {
	Summary               stateShowSummary             `json:"summary"`
	Sessions              []stateShowSession           `json:"sessions"`
	AdoptionQueue         []stateShowAdoption          `json:"adoption_queue,omitempty"`
	ProposalAdoptionQueue []stateShowProposalAdoption  `json:"proposal_adoption_queue,omitempty"`
	AgentTasks            []stateShowAgentTask         `json:"agent_tasks,omitempty"`
	AgentTaskDecisions    []stateShowAgentTaskDecision `json:"agent_task_decisions,omitempty"`
	AgentTaskAttempts     []stateShowAgentTaskAttempt  `json:"agent_task_attempts,omitempty"`
	RecentActions         []stateShowAction            `json:"recent_actions,omitempty"`
}

var stateShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show persisted state from database",
	RunE:  runStateShow,
}

var stateShowJSON bool

func init() {
	stateCmd.AddCommand(stateShowCmd)
	stateShowCmd.Flags().BoolVar(&stateShowJSON, "json", false, "Output machine-readable JSON")
}

func runStateShow(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	report, err := buildStateShowReport(db)
	if err != nil {
		return err
	}

	if stateShowJSON {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding json: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("=== Sessions (%d active, %d archived, %d queued adoptions, %d managed repos, %d task proposals, %d queued proposal adoptions, %d agent tasks, %d decisions, %d attempts) ===\n",
		report.Summary.ActiveSessions, report.Summary.ArchivedSessions, report.Summary.QueuedAdoptions, report.Summary.ManagedRepos, report.Summary.TaskProposals, report.Summary.QueuedProposalAdoptions, report.Summary.AgentTasks, report.Summary.AgentTaskDecisions, report.Summary.AgentTaskAttempts)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tREPOSITORY\tBRANCH\tSTATUS\tDESIRED\tRUNTIME\tHEALTH\tLAST ACTIVE\tPR\tTASK")
	for _, s := range report.Sessions {
		age := "-"
		if !s.LastActive.IsZero() {
			age = formatAge(time.Since(s.LastActive))
		}
		task := s.TaskSummary
		if task == "" {
			task = "-"
		}
		branch := s.Branch
		if branch == "" {
			branch = "-"
		}
		status := s.Status
		if s.BlockedReason != "" {
			status = s.Status + "(" + s.BlockedReason + ")"
		}
		if s.Duplicate {
			status = status + " [dup]"
		}
		if s.Ghost {
			status = status + " [ghost]"
		}
		health := "-"
		if len(s.Health) > 0 {
			health = strings.Join(s.Health, ",")
		}
		pr := s.PRURL
		if pr == "" {
			pr = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Agent, s.Repository, branch, status, s.DesiredState, s.RuntimeStatus, health, age, pr, task)
	}
	w.Flush()

	if len(report.AdoptionQueue) > 0 {
		fmt.Println("\n=== Queued Adoptions ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tAGENT\tMUX\tSESSION\tREPOSITORY\tBRANCH\tSTRATEGY\tPERMISSION\tNOTE")
		for _, a := range report.AdoptionQueue {
			repository := a.Repository
			if repository == "" {
				repository = "-"
			}
			branch := a.Branch
			if branch == "" {
				branch = "-"
			}
			note := a.Note
			if note == "" {
				note = "-"
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				a.ID, a.Agent, a.Mux, a.ZellijSession, repository, branch, a.Strategy, a.TargetPermission, note)
		}
		w.Flush()
	}

	if len(report.ProposalAdoptionQueue) > 0 {
		fmt.Println("\n=== Queued Proposal Adoptions ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSNAPSHOT\tREPO\tCATEGORY\tTASK_TYPE\tRISK\tAPPROVAL\tNOTE")
		for _, a := range report.ProposalAdoptionQueue {
			note := a.OperatorNote
			if note == "" {
				note = "-"
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				a.ID, a.ProposalSnapshot, a.RepoRef, a.Category, a.TaskType, a.Risk, a.ApprovalStatus, note)
		}
		w.Flush()
	}

	if len(report.AgentTasks) > 0 {
		fmt.Println("\n=== Agent Tasks ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tREPO\tTASK_TYPE\tRISK\tSTATUS\tSOURCE_KIND")
		for _, a := range report.AgentTasks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				a.Name, a.RepoRef, a.TaskType, a.Risk, a.Status, a.SourceKind)
		}
		w.Flush()
	}

	if len(report.AgentTaskDecisions) > 0 {
		fmt.Println("\n=== Agent Task Decisions ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tAGENT_TASK\tREPO\tTASK_TYPE\tRISK\tAGENT\tREPO_MODE\tSTATUS")
		for _, decision := range report.AgentTaskDecisions {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				decision.ID,
				decision.AgentTaskName,
				decision.RepoRef,
				decision.TaskType,
				decision.Risk,
				decision.SelectedAgent,
				decision.SelectedRepoMode,
				decision.Status)
		}
		w.Flush()
	}

	if len(report.AgentTaskAttempts) > 0 {
		fmt.Println("\n=== Agent Task Attempts ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tDECISION\tAGENT_TASK\tAGENT\tREPO_MODE\tSTATUS\tSESSION")
		for _, attempt := range report.AgentTaskAttempts {
			fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
				attempt.ID,
				attempt.DecisionID,
				attempt.AgentTaskName,
				attempt.Agent,
				attempt.RepoMode,
				attempt.Status,
				dashIfEmpty(attempt.SessionName))
		}
		w.Flush()
	}

	// Active tasks
	tasks, err := store.GetActiveTasks(db)
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}
	if len(tasks) > 0 {
		fmt.Println("\n=== Active Tasks ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSESSION\tSTATUS\tDESCRIPTION")
		for _, t := range tasks {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", t.ID, t.SessionID, t.Status, t.Description)
		}
		w.Flush()
	}

	// Recent actions
	actions, err := store.GetRecentActions(db, 10)
	if err != nil {
		return fmt.Errorf("listing actions: %w", err)
	}
	if len(actions) > 0 {
		fmt.Println("\n=== Recent Actions ===")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tTYPE\tSESSION\tCONTENT\tROUTE_REASON\tHANDOFF_SUMMARY\tTOKEN_BURN")
		for _, a := range actions {
			content := truncateForTable(a.Content, 80)
			routeReason := truncateForTable(a.RouteReason, 32)
			handoffSummary := truncateForTable(a.HandoffSummary, 40)
			tokenBurn := "-"
			if a.TokenBurn > 0 {
				tokenBurn = fmt.Sprintf("%d", a.TokenBurn)
			}
			session := a.SessionID
			if session == "" {
				session = "-"
			}
			if routeReason == "" {
				routeReason = "-"
			}
			if handoffSummary == "" {
				handoffSummary = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				a.CreatedAt.Format("15:04:05"), a.ActionType, session, content, routeReason, handoffSummary, tokenBurn)
		}
		w.Flush()
	}

	// Manager state
	state, err := store.AllState(db)
	if err != nil {
		return fmt.Errorf("listing state: %w", err)
	}
	if len(state) > 0 {
		fmt.Println("\n=== Manager State ===")
		for k, v := range state {
			fmt.Printf("  %s = %s\n", k, v)
		}
	}

	return nil
}

func buildStateShowReport(db *sql.DB) (*stateShowReport, error) {
	sessions, err := store.ListSessions(db)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	archiveCount, _ := store.GetArchivedSessionCount(db)

	duplicateCounts := map[string]int{}
	for _, s := range sessions {
		if s.ZellijSession == "" {
			continue
		}
		duplicateCounts[strings.ToLower(s.ZellijSession)]++
	}

	report := &stateShowReport{
		Summary: stateShowSummary{
			ActiveSessions:   len(sessions),
			ArchivedSessions: archiveCount,
		},
	}

	for _, s := range sessions {
		dupCount := 0
		dupGroup := ""
		duplicate := false
		if s.ZellijSession != "" {
			dupGroup = strings.ToLower(s.ZellijSession)
			dupCount = duplicateCounts[dupGroup]
			duplicate = dupCount > 1
		}
		ghost := s.WantsRunning() && s.RuntimeStatus != "running"

		health := []string{}
		switch s.Status {
		case "blocked":
			health = append(health, "blocked")
			report.Summary.BlockedSessions++
		case "error":
			health = append(health, "error")
			report.Summary.ErrorSessions++
		}
		if !s.WantsRunning() {
			report.Summary.DeadSessions++
		}
		if ghost {
			report.Summary.GhostSessions++
			health = append(health, "ghost")
		}
		if duplicate {
			report.Summary.DuplicateSessions++
			health = append(health, "duplicate")
		}

		branch := s.GitBranch
		if branch == "" {
			branch = "-"
		}
		report.Sessions = append(report.Sessions, stateShowSession{
			ID:             s.ID,
			Agent:          s.Agent,
			Repository:     s.Repository,
			Branch:         branch,
			Status:         s.Status,
			BlockedReason:  s.BlockedReason,
			DesiredState:   s.DesiredState,
			Alive:          s.WantsRunning(),
			RuntimeStatus:  s.RuntimeStatus,
			ZellijSession:  s.ZellijSession,
			TaskSummary:    s.TaskSummary,
			PRURL:          s.PRURL,
			LastActive:     s.LastActive,
			Ghost:          ghost,
			Duplicate:      duplicate,
			DuplicateGroup: dupGroup,
			DuplicateCount: dupCount,
			Health:         health,
		})
	}

	actions, err := store.GetRecentActions(db, 10)
	if err != nil {
		return nil, fmt.Errorf("listing actions: %w", err)
	}
	for _, a := range actions {
		report.RecentActions = append(report.RecentActions, stateShowAction{
			ID:             a.ID,
			SessionID:      a.SessionID,
			ActionType:     a.ActionType,
			Content:        a.Content,
			Result:         a.Result,
			RouteReason:    a.RouteReason,
			HandoffSummary: a.HandoffSummary,
			TokenBurn:      a.TokenBurn,
			CreatedAt:      a.CreatedAt,
		})
	}

	for _, count := range duplicateCounts {
		if count > 1 {
			report.Summary.DuplicateGroups++
		}
	}

	queuedAdoptions, err := store.ListQueuedSessionAdoptions(db)
	if err != nil {
		return nil, fmt.Errorf("listing queued adoptions: %w", err)
	}
	report.Summary.QueuedAdoptions = len(queuedAdoptions)
	for _, adoption := range queuedAdoptions {
		report.AdoptionQueue = append(report.AdoptionQueue, stateShowAdoption{
			ID:               adoption.ID,
			Agent:            adoption.Agent,
			Mux:              adoption.Mux,
			ZellijSession:    adoption.ZellijSession,
			ExternalSession:  adoption.ExternalSessionID,
			Repository:       adoption.Repository,
			Branch:           adoption.GitBranch,
			CWD:              adoption.CWD,
			Strategy:         adoption.Strategy,
			TargetPermission: fmt.Sprintf("%d (%s)", adoption.TargetPermissionLevel, store.PermissionLabel(adoption.TargetPermissionLevel)),
			Status:           adoption.Status,
			Note:             adoption.Note,
			CreatedAt:        adoption.CreatedAt,
		})
	}

	queuedProposalAdoptions, err := store.ListQueuedTaskProposalAdoptions(db)
	if err != nil {
		return nil, fmt.Errorf("listing queued task proposal adoptions: %w", err)
	}
	report.Summary.QueuedProposalAdoptions = len(queuedProposalAdoptions)
	for _, adoption := range queuedProposalAdoptions {
		report.ProposalAdoptionQueue = append(report.ProposalAdoptionQueue, stateShowProposalAdoption{
			ID:               adoption.ID,
			ProposalSnapshot: adoption.ProposalSnapshotID,
			RepoRef:          adoption.RepoRef,
			Repository:       adoption.Repository,
			Category:         adoption.Category,
			TaskType:         adoption.TaskType,
			Risk:             adoption.Risk,
			Status:           adoption.Status,
			ApprovalStatus:   adoption.ApprovalStatus,
			OperatorNote:     adoption.OperatorNote,
			DesiredOutcome:   adoption.DesiredOutcome,
			CreatedAt:        adoption.CreatedAt,
		})
	}

	managedRepos, err := store.ListManagedRepos(db)
	if err != nil {
		return nil, fmt.Errorf("listing managed repos: %w", err)
	}
	report.Summary.ManagedRepos = len(managedRepos)

	taskProposals, err := store.CountTaskProposalSnapshots(db)
	if err != nil {
		return nil, fmt.Errorf("counting task proposal snapshots: %w", err)
	}
	report.Summary.TaskProposals = taskProposals

	agentTasks, err := store.ListAgentTasks(db)
	if err != nil {
		return nil, fmt.Errorf("listing agent tasks: %w", err)
	}
	report.Summary.AgentTasks = len(agentTasks)
	for _, task := range agentTasks {
		report.AgentTasks = append(report.AgentTasks, stateShowAgentTask{
			Name:        task.Name,
			RepoRef:     task.RepoRef,
			Repository:  task.Repository,
			TaskType:    task.TaskType,
			Risk:        task.Risk,
			Status:      task.Status,
			SourceKind:  task.SourceKind,
			ContextRefs: task.ContextRefs,
			CreatedAt:   task.CreatedAt,
		})
	}

	decisions, err := store.ListAgentTaskDecisions(db)
	if err != nil {
		return nil, fmt.Errorf("listing agent task decisions: %w", err)
	}
	report.Summary.AgentTaskDecisions = len(decisions)
	for _, decision := range decisions {
		report.AgentTaskDecisions = append(report.AgentTaskDecisions, stateShowAgentTaskDecision{
			ID:               decision.ID,
			AgentTaskName:    decision.AgentTaskName,
			RepoRef:          decision.RepoRef,
			Repository:       decision.Repository,
			TaskType:         decision.TaskType,
			Risk:             decision.Risk,
			SelectedAgent:    decision.SelectedAgent,
			SelectedRepoMode: decision.SelectedRepoMode,
			Status:           decision.Status,
			EligibleAgents:   decision.EligibleAgents,
			RouteReason:      decision.RouteReason,
			CreatedAt:        decision.CreatedAt,
		})
	}

	attempts, err := store.ListAgentTaskAttempts(db)
	if err != nil {
		return nil, fmt.Errorf("listing agent task attempts: %w", err)
	}
	report.Summary.AgentTaskAttempts = len(attempts)
	for _, attempt := range attempts {
		report.AgentTaskAttempts = append(report.AgentTaskAttempts, stateShowAgentTaskAttempt{
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
			Status:           attempt.Status,
			FailureReason:    attempt.FailureReason,
			CreatedAt:        attempt.CreatedAt,
		})
	}

	return report, nil
}

func truncateForTable(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
