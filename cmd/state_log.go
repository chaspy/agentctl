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
	logLimit          int
	logSince          string
	logSession        string
	logRouteReason    string
	logHandoffSummary string
	logTokenBurn      int
)

var stateLogCmd = &cobra.Command{
	Use:   "log [message]",
	Short: "View or add action log entries",
	Long:  "Without arguments: show recent actions. With arguments: add a note to the log.",
	RunE:  runStateLog,
}

var stateLogHandoffCmd = &cobra.Command{
	Use:   "handoff <session-id>",
	Short: "Record route reason, handoff summary, and token burn for a worker session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateLogHandoff,
}

func init() {
	stateCmd.AddCommand(stateLogCmd)
	stateLogCmd.AddCommand(stateLogHandoffCmd)
	stateLogCmd.Flags().IntVar(&logLimit, "limit", 20, "Number of entries to show")
	stateLogCmd.Flags().StringVar(&logSince, "since", "", "Show actions since duration (e.g., 1h, 30m)")
	stateLogCmd.Flags().StringVar(&logSession, "session", "", "Filter by session ID")
	stateLogHandoffCmd.Flags().StringVar(&logRouteReason, "route-reason", "", "Why this worker was routed or selected")
	stateLogHandoffCmd.Flags().StringVar(&logHandoffSummary, "handoff-summary", "", "Summary for the next worker or reviewer")
	stateLogHandoffCmd.Flags().IntVar(&logTokenBurn, "token-burn", 0, "Approximate tokens consumed during the session")
	_ = stateLogHandoffCmd.MarkFlagRequired("route-reason")
	_ = stateLogHandoffCmd.MarkFlagRequired("handoff-summary")
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
	fmt.Fprintln(w, "TIME\tTYPE\tSESSION\tCONTENT\tRESULT\tROUTE_REASON\tHANDOFF_SUMMARY\tTOKEN_BURN")
	for _, a := range actions {
		content := truncateForTable(a.Content, 60)
		result := truncateForTable(a.Result, 40)
		if result == "" {
			result = "-"
		}
		routeReason := truncateForTable(a.RouteReason, 32)
		if routeReason == "" {
			routeReason = "-"
		}
		handoffSummary := truncateForTable(a.HandoffSummary, 40)
		if handoffSummary == "" {
			handoffSummary = "-"
		}
		tokenBurn := "-"
		if a.TokenBurn > 0 {
			tokenBurn = fmt.Sprintf("%d", a.TokenBurn)
		}
		sess := a.SessionID
		if sess == "" {
			sess = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			a.CreatedAt.Format("01/02 15:04"), a.ActionType, sess, content, result, routeReason, handoffSummary, tokenBurn)
	}
	return w.Flush()
}

func runStateLogHandoff(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if logTokenBurn < 0 {
		return fmt.Errorf("token-burn must be >= 0")
	}

	sessionID := args[0]
	action := &store.Action{
		SessionID:      sessionID,
		ActionType:     "handoff",
		Content:        "worker session handoff recorded",
		RouteReason:    logRouteReason,
		HandoffSummary: logHandoffSummary,
		TokenBurn:      logTokenBurn,
	}
	if err := store.LogAction(db, action); err != nil {
		return fmt.Errorf("logging handoff: %w", err)
	}

	fmt.Printf("Logged handoff for %s\n", sessionID)
	return nil
}
