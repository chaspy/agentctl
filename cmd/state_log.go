package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	logLimit   int
	logSince   string
	logSession string
)

var stateLogCmd = &cobra.Command{
	Use:   "log [message]",
	Short: "View or add action log entries",
	Long:  "Without arguments: show recent actions. With arguments: add a note to the log.",
	RunE:  runStateLog,
}

func init() {
	stateCmd.AddCommand(stateLogCmd)
	stateLogCmd.Flags().IntVar(&logLimit, "limit", 20, "Number of entries to show")
	stateLogCmd.Flags().StringVar(&logSince, "since", "", "Show actions since duration (e.g., 1h, 30m)")
	stateLogCmd.Flags().StringVar(&logSession, "session", "", "Filter by session ID")
}

func runStateLog(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// If arguments provided, add as a note
	if len(args) > 0 {
		message := strings.Join(args, " ")
		a := &store.Action{
			ActionType: "note",
			SessionID:  logSession,
			Content:    message,
		}
		if err := store.LogAction(db, a); err != nil {
			return fmt.Errorf("logging action: %w", err)
		}
		fmt.Printf("Logged note: %s\n", message)
		return nil
	}

	// Show recent actions
	var actions []store.Action
	if logSince != "" {
		d, err := time.ParseDuration(logSince)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", logSince, err)
		}
		actions, err = store.GetActionsSince(db, time.Now().Add(-d))
		if err != nil {
			return fmt.Errorf("querying actions: %w", err)
		}
	} else if logSession != "" {
		actions, err = store.GetActionsForSession(db, logSession, logLimit)
		if err != nil {
			return fmt.Errorf("querying actions: %w", err)
		}
	} else {
		actions, err = store.GetRecentActions(db, logLimit)
		if err != nil {
			return fmt.Errorf("querying actions: %w", err)
		}
	}

	if len(actions) == 0 {
		fmt.Println("No actions found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tTYPE\tSESSION\tCONTENT\tRESULT")
	for _, a := range actions {
		content := a.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		result := a.Result
		if len(result) > 40 {
			result = result[:40] + "..."
		}
		if result == "" {
			result = "-"
		}
		sess := a.SessionID
		if sess == "" {
			sess = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			a.CreatedAt.Format("01/02 15:04"), a.ActionType, sess, content, result)
	}
	return w.Flush()
}
