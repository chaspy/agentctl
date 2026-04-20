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
	ActiveSessions    int `json:"active_sessions"`
	ArchivedSessions  int `json:"archived_sessions"`
	QueuedAdoptions   int `json:"queued_adoptions"`
	ManagedRepos      int `json:"managed_repos"`
	TaskProposals     int `json:"task_proposals"`
	BlockedSessions   int `json:"blocked_sessions"`
	ErrorSessions     int `json:"error_sessions"`
	GhostSessions     int `json:"ghost_sessions"`
	DuplicateSessions int `json:"duplicate_sessions"`
	DuplicateGroups   int `json:"duplicate_groups"`
	DeadSessions      int `json:"dead_sessions"`
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

type stateShowReport struct {
	Summary       stateShowSummary    `json:"summary"`
	Sessions      []stateShowSession  `json:"sessions"`
	AdoptionQueue []stateShowAdoption `json:"adoption_queue,omitempty"`
	RecentActions []stateShowAction   `json:"recent_actions,omitempty"`
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

	fmt.Printf("=== Sessions (%d active, %d archived, %d queued adoptions, %d managed repos, %d task proposals) ===\n",
		report.Summary.ActiveSessions, report.Summary.ArchivedSessions, report.Summary.QueuedAdoptions, report.Summary.ManagedRepos, report.Summary.TaskProposals)
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

	return report, nil
}

func truncateForTable(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
