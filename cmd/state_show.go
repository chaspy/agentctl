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

type stateShowReport struct {
	Summary  stateShowSummary   `json:"summary"`
	Sessions []stateShowSession `json:"sessions"`
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

	fmt.Printf("=== Sessions (%d active, %d archived) ===\n", report.Summary.ActiveSessions, report.Summary.ArchivedSessions)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tREPOSITORY\tBRANCH\tSTATUS\tALIVE\tHEALTH\tLAST ACTIVE\tPR\tTASK")
	for _, s := range report.Sessions {
		alive := "no"
		if s.Alive {
			alive = "yes"
		}
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Agent, s.Repository, branch, status, alive, health, age, pr, task)
	}
	w.Flush()

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
		fmt.Fprintln(w, "TIME\tTYPE\tSESSION\tCONTENT")
		for _, a := range actions {
			content := a.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			session := a.SessionID
			if session == "" {
				session = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				a.CreatedAt.Format("15:04:05"), a.ActionType, session, content)
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
		ghost := s.Alive && s.RuntimeStatus != "running"

		health := []string{}
		switch s.Status {
		case "blocked":
			health = append(health, "blocked")
			report.Summary.BlockedSessions++
		case "error":
			health = append(health, "error")
			report.Summary.ErrorSessions++
		}
		if !s.Alive {
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
			Alive:          s.Alive,
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

	for _, count := range duplicateCounts {
		if count > 1 {
			report.Summary.DuplicateGroups++
		}
	}

	return report, nil
}
