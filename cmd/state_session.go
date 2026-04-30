package cmd

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	sessionSummaryText         string
	sessionMarkDeadReason      string
	sessionMarkDeadLogAction   bool
	sessionMarkDeadRouteReason string
	sessionMarkDeadResult      string
)

var stateSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Mutate persisted session state safely through agentctl",
}

var stateSessionSummaryCmd = &cobra.Command{
	Use:   "summary <session-key>",
	Short: "Update task_summary for an active session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateSessionSummary,
}

var stateSessionClearSummariesCmd = &cobra.Command{
	Use:   "clear-summaries",
	Short: "Clear task_summary for active sessions",
	RunE:  runStateSessionClearSummaries,
}

var stateSessionMarkDeadCmd = &cobra.Command{
	Use:   "mark-dead <session-key>",
	Short: "Mark an active session dead and optionally log the action",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateSessionMarkDead,
}

func init() {
	stateCmd.AddCommand(stateSessionCmd)
	stateSessionCmd.AddCommand(stateSessionSummaryCmd)
	stateSessionCmd.AddCommand(stateSessionClearSummariesCmd)
	stateSessionCmd.AddCommand(stateSessionMarkDeadCmd)

	stateSessionSummaryCmd.Flags().StringVar(&sessionSummaryText, "text", "", "Replacement task summary text")
	_ = stateSessionSummaryCmd.MarkFlagRequired("text")

	stateSessionMarkDeadCmd.Flags().StringVar(&sessionMarkDeadReason, "reason", "", "Reason recorded for the state transition")
	stateSessionMarkDeadCmd.Flags().BoolVar(&sessionMarkDeadLogAction, "log-action", false, "Log an action entry for the state transition")
	stateSessionMarkDeadCmd.Flags().StringVar(&sessionMarkDeadRouteReason, "route-reason", "", "Optional route reason recorded with the action log")
	stateSessionMarkDeadCmd.Flags().StringVar(&sessionMarkDeadResult, "result", "", "Optional action result recorded with the action log")
}

func runStateSessionSummary(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	session, err := store.GetActiveSessionByKey(db, strings.TrimSpace(args[0]))
	if err == sql.ErrNoRows {
		return fmt.Errorf("active session not found: %s", args[0])
	}
	if err != nil {
		return fmt.Errorf("resolving session: %w", err)
	}

	if err := store.UpdateTaskSummary(db, session.ID, sessionSummaryText); err != nil {
		return fmt.Errorf("updating task summary: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updated task summary for %s\n", session.ID)
	return nil
}

func runStateSessionClearSummaries(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	rows, err := store.ClearTaskSummariesForActiveSessions(db)
	if err != nil {
		return fmt.Errorf("clearing task summaries: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Cleared %d session summaries\n", rows)
	return nil
}

func runStateSessionMarkDead(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	session, err := store.GetActiveSessionByKey(db, strings.TrimSpace(args[0]))
	if err == sql.ErrNoRows {
		return fmt.Errorf("active session not found: %s", args[0])
	}
	if err != nil {
		return fmt.Errorf("resolving session: %w", err)
	}
	if strings.TrimSpace(session.ZellijSession) == "" {
		return fmt.Errorf("session %s has no zellij session", session.ID)
	}

	rows, err := store.MarkSessionDeadByZellijSession(db, session.ZellijSession)
	if err != nil {
		return fmt.Errorf("marking session dead: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("active session not found: %s", args[0])
	}

	if sessionMarkDeadLogAction {
		action := &store.Action{
			SessionID:   session.ID,
			ActionType:  "kill",
			Content:     firstNonEmpty(strings.TrimSpace(sessionMarkDeadReason), "session marked dead"),
			Result:      firstNonEmpty(strings.TrimSpace(sessionMarkDeadResult), "session_mark_dead"),
			RouteReason: strings.TrimSpace(sessionMarkDeadRouteReason),
		}
		if err := store.LogAction(db, action); err != nil {
			return fmt.Errorf("logging action: %w", err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Marked %d session(s) dead for %s\n", rows, session.ZellijSession)
	return nil
}
