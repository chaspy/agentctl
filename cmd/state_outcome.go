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

var stateOutcomeJSON bool

var stateOutcomeCmd = &cobra.Command{
	Use:   "outcome",
	Short: "Inspect recorded AgentTask outcomes",
}

var stateOutcomeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AgentTask outcomes from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateOutcomeList,
}

var stateOutcomeGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one AgentTask outcome from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateOutcomeGet,
}

func init() {
	stateCmd.AddCommand(stateOutcomeCmd)
	stateOutcomeCmd.AddCommand(stateOutcomeListCmd)
	stateOutcomeCmd.AddCommand(stateOutcomeGetCmd)
	stateOutcomeListCmd.Flags().BoolVar(&stateOutcomeJSON, "json", false, "Output machine-readable JSON")
	stateOutcomeGetCmd.Flags().BoolVar(&stateOutcomeJSON, "json", false, "Output machine-readable JSON")
}

func runStateOutcomeList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	outcomes, err := store.ListAgentTaskOutcomes(db)
	if err != nil {
		return fmt.Errorf("listing outcomes: %w", err)
	}
	if stateOutcomeJSON {
		return writeOutcomeJSON(cmd.OutOrStdout(), outcomes)
	}
	if len(outcomes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No outcomes found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tATTEMPT\tAGENT_TASK\tSTATUS\tPR\tSOURCE\tCREATED")
	for _, outcome := range outcomes {
		pr := outcome.PRURL
		if pr == "" {
			pr = "-"
		}
		created := outcome.CreatedAt
		if created == "" {
			created = "-"
		}
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			outcome.ID,
			outcome.AttemptID,
			outcome.AgentTaskName,
			outcome.Status,
			pr,
			dashIfEmpty(outcome.Source),
			created,
		)
	}
	return w.Flush()
}

func runStateOutcomeGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid outcome ID %q: %w", args[0], err)
	}
	outcome, err := store.GetAgentTaskOutcome(db, id)
	if err != nil {
		return fmt.Errorf("getting outcome %s: %w", args[0], err)
	}
	if outcome == nil {
		return fmt.Errorf("outcome %q not found", args[0])
	}
	if stateOutcomeJSON {
		return writeOutcomeJSON(cmd.OutOrStdout(), outcome)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:               %d\n", outcome.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "AttemptID:        %d\n", outcome.AttemptID)
	fmt.Fprintf(cmd.OutOrStdout(), "DecisionID:       %d\n", outcome.DecisionID)
	fmt.Fprintf(cmd.OutOrStdout(), "AgentTask:        %s\n", outcome.AgentTaskName)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:          %s\n", outcome.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:       %s\n", outcome.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:         %s\n", outcome.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:             %s\n", outcome.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "Agent:            %s\n", outcome.Agent)
	fmt.Fprintf(cmd.OutOrStdout(), "Branch:           %s\n", emptyFallback(outcome.Branch))
	fmt.Fprintf(cmd.OutOrStdout(), "SessionName:      %s\n", emptyFallback(outcome.SessionName))
	fmt.Fprintf(cmd.OutOrStdout(), "ManagedSessionID: %s\n", emptyFallback(outcome.ManagedSessionID))
	fmt.Fprintf(cmd.OutOrStdout(), "Status:           %s\n", outcome.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "ResultSummary:    %s\n", emptyFallback(outcome.ResultSummary))
	fmt.Fprintf(cmd.OutOrStdout(), "PRNumber:         %d\n", outcome.PRNumber)
	fmt.Fprintf(cmd.OutOrStdout(), "PRURL:            %s\n", emptyFallback(outcome.PRURL))
	fmt.Fprintf(cmd.OutOrStdout(), "PRState:          %s\n", emptyFallback(outcome.PRState))
	fmt.Fprintf(cmd.OutOrStdout(), "CommitSHA:        %s\n", emptyFallback(outcome.CommitSHA))
	fmt.Fprintf(cmd.OutOrStdout(), "FailureCategory:  %s\n", emptyFallback(outcome.FailureCategory))
	fmt.Fprintf(cmd.OutOrStdout(), "FailureReason:    %s\n", emptyFallback(outcome.FailureReason))
	fmt.Fprintf(cmd.OutOrStdout(), "Source:           %s\n", emptyFallback(outcome.Source))
	fmt.Fprintf(cmd.OutOrStdout(), "Created:          %s\n", emptyFallback(outcome.CreatedAt))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:          %s\n", emptyFallback(outcome.UpdatedAt))
	return nil
}

func writeOutcomeJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
