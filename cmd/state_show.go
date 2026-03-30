package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show persisted state from database",
	RunE:  runStateShow,
}

func init() {
	stateCmd.AddCommand(stateShowCmd)
}

func runStateShow(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Sessions
	sessions, err := store.ListSessions(db)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	archiveCount, _ := store.GetArchivedSessionCount(db)

	fmt.Printf("=== Sessions (%d active, %d archived) ===\n", len(sessions), archiveCount)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tREPOSITORY\tBRANCH\tSTATUS\tALIVE\tLAST ACTIVE\tPR\tTASK")
	for _, s := range sessions {
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
		branch := s.GitBranch
		if branch == "" {
			branch = "-"
		}
		status := s.Status
		if s.BlockedReason != "" {
			status = s.Status + "(" + s.BlockedReason + ")"
		}
		if s.IsLoop {
			status = status + " 🔁"
		}
		pr := s.PRURL
		if pr == "" {
			pr = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Agent, s.Repository, branch, status, alive, age, pr, task)
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
