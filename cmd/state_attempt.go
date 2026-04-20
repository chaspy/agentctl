package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateAttemptJSON bool

var stateAttemptCmd = &cobra.Command{
	Use:   "attempt",
	Short: "Inspect recorded AgentTask attempts",
}

var stateAttemptListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AgentTask attempts from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateAttemptList,
}

var stateAttemptGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one AgentTask attempt from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateAttemptGet,
}

func init() {
	stateCmd.AddCommand(stateAttemptCmd)
	stateAttemptCmd.AddCommand(stateAttemptListCmd)
	stateAttemptCmd.AddCommand(stateAttemptGetCmd)
	stateAttemptListCmd.Flags().BoolVar(&stateAttemptJSON, "json", false, "Output machine-readable JSON")
	stateAttemptGetCmd.Flags().BoolVar(&stateAttemptJSON, "json", false, "Output machine-readable JSON")
}

func runStateAttemptList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	attempts, err := store.ListAgentTaskAttempts(db)
	if err != nil {
		return fmt.Errorf("listing attempts: %w", err)
	}
	if stateAttemptJSON {
		return writeAttemptJSON(cmd.OutOrStdout(), attempts)
	}
	if len(attempts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No attempts found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDECISION\tAGENT_TASK\tAGENT\tREPO_MODE\tSTATUS\tSESSION\tCREATED")
	for _, attempt := range attempts {
		created := attempt.CreatedAt
		if created == "" {
			created = "-"
		}
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			attempt.ID,
			attempt.DecisionID,
			attempt.AgentTaskName,
			attempt.Agent,
			attempt.RepoMode,
			attempt.Status,
			dashIfEmpty(attempt.SessionName),
			created,
		)
	}
	return w.Flush()
}

func runStateAttemptGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid attempt ID %q: %w", args[0], err)
	}
	attempt, err := store.GetAgentTaskAttempt(db, id)
	if err != nil {
		return fmt.Errorf("getting attempt %s: %w", args[0], err)
	}
	if attempt == nil {
		return fmt.Errorf("attempt %q not found", args[0])
	}
	if stateAttemptJSON {
		return writeAttemptJSON(cmd.OutOrStdout(), attempt)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:               %d\n", attempt.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "DecisionID:       %d\n", attempt.DecisionID)
	fmt.Fprintf(cmd.OutOrStdout(), "AgentTask:        %s\n", attempt.AgentTaskName)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:          %s\n", attempt.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:       %s\n", attempt.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:         %s\n", attempt.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:             %s\n", attempt.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "Agent:            %s\n", attempt.Agent)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoMode:         %s\n", emptyFallback(attempt.RepoMode))
	fmt.Fprintf(cmd.OutOrStdout(), "Branch:           %s\n", emptyFallback(attempt.Branch))
	fmt.Fprintf(cmd.OutOrStdout(), "SessionName:      %s\n", emptyFallback(attempt.SessionName))
	fmt.Fprintf(cmd.OutOrStdout(), "ManagedSessionID: %s\n", emptyFallback(attempt.ManagedSessionID))
	fmt.Fprintf(cmd.OutOrStdout(), "WorkDir:          %s\n", emptyFallback(attempt.WorkDir))
	fmt.Fprintf(cmd.OutOrStdout(), "LaunchCommand:    %s\n", emptyFallback(attempt.LaunchCommand))
	fmt.Fprintf(cmd.OutOrStdout(), "InitialMessage:   %s\n", emptyFallback(attempt.InitialMessage))
	fmt.Fprintf(cmd.OutOrStdout(), "Summary:          %s\n", emptyFallback(attempt.Summary))
	fmt.Fprintf(cmd.OutOrStdout(), "Status:           %s\n", attempt.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "FailureReason:    %s\n", emptyFallback(attempt.FailureReason))
	fmt.Fprintf(cmd.OutOrStdout(), "Created:          %s\n", emptyFallback(attempt.CreatedAt))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:          %s\n", emptyFallback(attempt.UpdatedAt))
	return nil
}

func writeAttemptJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
