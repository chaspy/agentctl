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
var (
	stateAttemptOutcomeDryRun          bool
	stateAttemptOutcomeStatus          string
	stateAttemptOutcomeSummary         string
	stateAttemptOutcomePRNumber        int
	stateAttemptOutcomePRURL           string
	stateAttemptOutcomePRState         string
	stateAttemptOutcomeCommitSHA       string
	stateAttemptOutcomeFailureCategory string
	stateAttemptOutcomeFailureReason   string
	stateAttemptOutcomeSource          string
)

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

var stateAttemptOutcomeCmd = &cobra.Command{
	Use:   "outcome <id>",
	Short: "Record a final Outcome from one AgentTask attempt",
	Long: `Builds and stores a final Outcome from one recorded Attempt.
PR fields are hydrated from the linked managed session when available, and explicit flags override those observed values.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateAttemptOutcome,
}

func init() {
	stateCmd.AddCommand(stateAttemptCmd)
	stateAttemptCmd.AddCommand(stateAttemptListCmd)
	stateAttemptCmd.AddCommand(stateAttemptGetCmd)
	stateAttemptCmd.AddCommand(stateAttemptOutcomeCmd)
	stateAttemptListCmd.Flags().BoolVar(&stateAttemptJSON, "json", false, "Output machine-readable JSON")
	stateAttemptGetCmd.Flags().BoolVar(&stateAttemptJSON, "json", false, "Output machine-readable JSON")
	stateAttemptOutcomeCmd.Flags().BoolVar(&stateAttemptOutcomeDryRun, "dry-run", false, "Preview the outcome without writing to SQLite")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeStatus, "status", "", "Final outcome status: completed, failed, cancelled")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeSummary, "summary", "", "Final outcome summary override")
	stateAttemptOutcomeCmd.Flags().IntVar(&stateAttemptOutcomePRNumber, "pr-number", 0, "PR number override")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomePRURL, "pr-url", "", "PR URL override")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomePRState, "pr-state", "", "PR state override")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeCommitSHA, "commit-sha", "", "Commit SHA override")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeFailureCategory, "failure-category", "", "Failure category for failed outcomes")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeFailureReason, "failure-reason", "", "Failure reason for failed outcomes")
	stateAttemptOutcomeCmd.Flags().StringVar(&stateAttemptOutcomeSource, "source", "", "Outcome source label override")
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

func runStateAttemptOutcome(cmd *cobra.Command, args []string) error {
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

	existing, err := store.GetAgentTaskOutcomeByAttemptID(db, attempt.ID)
	if err != nil {
		return fmt.Errorf("checking existing outcome for attempt %d: %w", attempt.ID, err)
	}
	if existing != nil {
		return fmt.Errorf("attempt %d already has outcome #%d", attempt.ID, existing.ID)
	}

	plan, err := buildAgentTaskOutcomePlan(
		db,
		attempt,
		stateAttemptOutcomeStatus,
		stateAttemptOutcomeSummary,
		stateAttemptOutcomePRNumber,
		stateAttemptOutcomePRURL,
		stateAttemptOutcomePRState,
		stateAttemptOutcomeCommitSHA,
		stateAttemptOutcomeFailureCategory,
		stateAttemptOutcomeFailureReason,
		stateAttemptOutcomeSource,
	)
	if err != nil {
		return fmt.Errorf("building outcome plan: %w", err)
	}
	if stateAttemptOutcomeDryRun {
		writeAgentTaskOutcomePlan(cmd.OutOrStdout(), plan.Outcome, true)
		fmt.Fprintln(cmd.OutOrStdout(), "No database changes were made.")
		return nil
	}

	if err := store.CreateAgentTaskOutcome(db, plan.Outcome); err != nil {
		return fmt.Errorf("creating outcome: %w", err)
	}
	if err := store.UpdateAgentTaskDecisionStatus(db, attempt.DecisionID, plan.Outcome.Status); err != nil {
		return fmt.Errorf("marking decision %d %s: %w", attempt.DecisionID, plan.Outcome.Status, err)
	}
	if err := store.UpdateAgentTaskStatus(db, attempt.AgentTaskName, plan.Outcome.Status); err != nil {
		return fmt.Errorf("marking agent task %s %s: %w", attempt.AgentTaskName, plan.Outcome.Status, err)
	}

	routeReason := ""
	decision, err := store.GetAgentTaskDecision(db, attempt.DecisionID)
	if err == nil && decision != nil {
		routeReason = decision.RouteReason
	}
	result := plan.Outcome.Status
	if plan.Outcome.PRURL != "" {
		result = plan.Outcome.PRURL
	}
	if err := store.LogAction(db, &store.Action{
		SessionID:   plan.Outcome.ManagedSessionID,
		ActionType:  "outcome",
		Content:     fmt.Sprintf("Recorded outcome #%d from attempt #%d for %s", plan.Outcome.ID, attempt.ID, attempt.AgentTaskName),
		Result:      result,
		RouteReason: routeReason,
	}); err != nil {
		return fmt.Errorf("logging outcome action: %w", err)
	}

	writeAgentTaskOutcomePlan(cmd.OutOrStdout(), plan.Outcome, false)
	fmt.Fprintf(cmd.OutOrStdout(), "Recorded outcome #%d from Attempt %d.\n", plan.Outcome.ID, attempt.ID)
	return nil
}

func writeAgentTaskOutcomePlan(w io.Writer, outcome *store.AgentTaskOutcome, dryRun bool) {
	title := "AgentTask outcome"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "attempt_id\t%d\n", outcome.AttemptID)
	fmt.Fprintf(tw, "decision_id\t%d\n", outcome.DecisionID)
	fmt.Fprintf(tw, "agent_task\t%s\n", outcome.AgentTaskName)
	fmt.Fprintf(tw, "repo\t%s\n", outcome.RepoRef)
	fmt.Fprintf(tw, "status\t%s\n", outcome.Status)
	fmt.Fprintf(tw, "result_summary\t%s\n", dashIfEmpty(outcome.ResultSummary))
	fmt.Fprintf(tw, "branch\t%s\n", dashIfEmpty(outcome.Branch))
	fmt.Fprintf(tw, "session_name\t%s\n", dashIfEmpty(outcome.SessionName))
	fmt.Fprintf(tw, "managed_session_id\t%s\n", dashIfEmpty(outcome.ManagedSessionID))
	if outcome.PRNumber > 0 {
		fmt.Fprintf(tw, "pr_number\t%d\n", outcome.PRNumber)
	} else {
		fmt.Fprintf(tw, "pr_number\t-\n")
	}
	fmt.Fprintf(tw, "pr_url\t%s\n", dashIfEmpty(outcome.PRURL))
	fmt.Fprintf(tw, "pr_state\t%s\n", dashIfEmpty(outcome.PRState))
	fmt.Fprintf(tw, "commit_sha\t%s\n", dashIfEmpty(outcome.CommitSHA))
	fmt.Fprintf(tw, "failure_category\t%s\n", dashIfEmpty(outcome.FailureCategory))
	fmt.Fprintf(tw, "failure_reason\t%s\n", dashIfEmpty(outcome.FailureReason))
	fmt.Fprintf(tw, "source\t%s\n", dashIfEmpty(outcome.Source))
	_ = tw.Flush()
}
