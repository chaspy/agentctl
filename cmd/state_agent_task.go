package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateAgentTaskJSON bool

var stateAgentTaskCmd = &cobra.Command{
	Use:   "agent-task",
	Short: "Inspect materialized AgentTask records",
}

var stateAgentTaskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List materialized AgentTask records from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateAgentTaskList,
}

var stateAgentTaskGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show one materialized AgentTask record from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateAgentTaskGet,
}

var (
	stateAgentTaskDecideDryRun bool
)

var stateAgentTaskDecideCmd = &cobra.Command{
	Use:   "decide <name>",
	Short: "Record a routing decision for one planned AgentTask",
	Long: `Computes and stores the current routing decision for an AgentTask.
This records the selected agent, repo mode, candidate scores, and route reason,
but does not spawn a session yet.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateAgentTaskDecide,
}

func init() {
	stateCmd.AddCommand(stateAgentTaskCmd)
	stateAgentTaskCmd.AddCommand(stateAgentTaskListCmd)
	stateAgentTaskCmd.AddCommand(stateAgentTaskGetCmd)
	stateAgentTaskCmd.AddCommand(stateAgentTaskDecideCmd)
	stateAgentTaskListCmd.Flags().BoolVar(&stateAgentTaskJSON, "json", false, "Output machine-readable JSON")
	stateAgentTaskGetCmd.Flags().BoolVar(&stateAgentTaskJSON, "json", false, "Output machine-readable JSON")
	stateAgentTaskDecideCmd.Flags().BoolVar(&stateAgentTaskDecideDryRun, "dry-run", false, "Preview the decision without writing to SQLite")
}

func runStateAgentTaskList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	tasks, err := store.ListAgentTasks(db)
	if err != nil {
		return fmt.Errorf("listing agent tasks: %w", err)
	}
	if stateAgentTaskJSON {
		return writeAgentTaskJSON(cmd.OutOrStdout(), tasks)
	}
	if len(tasks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No agent tasks found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPO\tTASK_TYPE\tRISK\tSTATUS\tSOURCE_KIND\tUPDATED")
	for _, task := range tasks {
		updated := task.UpdatedAt
		if updated == "" {
			updated = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			task.Name, task.RepoRef, task.TaskType, task.Risk, task.Status, task.SourceKind, updated)
	}
	return w.Flush()
}

func runStateAgentTaskGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	task, err := store.GetAgentTask(db, args[0])
	if err != nil {
		return fmt.Errorf("getting agent task %s: %w", args[0], err)
	}
	if task == nil {
		return fmt.Errorf("agent task %q not found", args[0])
	}
	if stateAgentTaskJSON {
		return writeAgentTaskJSON(cmd.OutOrStdout(), task)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Name:                       %s\n", task.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:                    %s\n", task.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:                 %s\n", task.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "Objective:                  %s\n", task.Objective)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:                   %s\n", task.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:                       %s\n", task.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "RoutingPolicyRef:           %s\n", emptyFallback(task.RoutingPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ReviewPolicyRef:            %s\n", emptyFallback(task.ReviewPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalPolicyRef:          %s\n", emptyFallback(task.ApprovalPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalRequiredBeforeMerge:%s\n", yesNo(task.ApprovalRequiredBeforeMerge))
	fmt.Fprintf(cmd.OutOrStdout(), "SourceKind:                 %s\n", task.SourceKind)
	fmt.Fprintf(cmd.OutOrStdout(), "SourceRef:                  %s\n", emptyFallback(task.SourceRef))
	fmt.Fprintf(cmd.OutOrStdout(), "Status:                     %s\n", task.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "SourcePath:                 %s\n", emptyFallback(task.SourcePath))
	fmt.Fprintf(cmd.OutOrStdout(), "SourceCommit:               %s\n", emptyFallback(task.SourceCommit))
	fmt.Fprintf(cmd.OutOrStdout(), "SpecHash:                   %s\n", emptyFallback(task.SpecHash))
	if len(task.ContextRefs) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "ContextRefs:")
		for _, item := range task.ContextRefs {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	if len(task.DesiredOutcome) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "DesiredOutcome:")
		for _, item := range task.DesiredOutcome {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:                    %s\n", emptyFallback(task.UpdatedAt))
	return nil
}

func runStateAgentTaskDecide(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	task, err := store.GetAgentTask(db, args[0])
	if err != nil {
		return fmt.Errorf("getting agent task %s: %w", args[0], err)
	}
	if task == nil {
		return fmt.Errorf("agent task %q not found", args[0])
	}
	if task.Status != "planned" {
		return fmt.Errorf("agent task %q is %q, expected planned", task.Name, task.Status)
	}

	decision, err := buildAgentTaskDecision(db, task)
	if err != nil {
		return fmt.Errorf("building decision for agent task %s: %w", task.Name, err)
	}
	if stateAgentTaskDecideDryRun {
		writeAgentTaskDecision(cmd.OutOrStdout(), decision, true)
		fmt.Fprintln(cmd.OutOrStdout(), "No database changes were made.")
		return nil
	}

	if err := store.CreateAgentTaskDecision(db, decision); err != nil {
		return fmt.Errorf("storing decision: %w", err)
	}
	if err := store.UpdateAgentTaskStatus(db, task.Name, "routed"); err != nil {
		return fmt.Errorf("marking agent task routed: %w", err)
	}
	if err := store.LogAction(db, &store.Action{
		ActionType:  "decision",
		Content:     fmt.Sprintf("Recorded decision #%d for agent task %s", decision.ID, task.Name),
		RouteReason: decision.RouteReason,
		Result:      decision.SelectedAgent,
	}); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("logging decision action: %w", err)
	}

	writeAgentTaskDecision(cmd.OutOrStdout(), decision, false)
	fmt.Fprintf(cmd.OutOrStdout(), "Recorded decision #%d for AgentTask %s.\n", decision.ID, task.Name)
	return nil
}

func writeAgentTaskJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func writeAgentTaskDecision(w io.Writer, decision *store.AgentTaskDecision, dryRun bool) {
	title := "AgentTask decision"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "agent_task\t%s\n", decision.AgentTaskName)
	fmt.Fprintf(tw, "repo\t%s\n", decision.RepoRef)
	fmt.Fprintf(tw, "task_type\t%s\n", decision.TaskType)
	fmt.Fprintf(tw, "risk\t%s\n", decision.Risk)
	fmt.Fprintf(tw, "routing_policy_ref\t%s\n", dashIfEmpty(decision.RoutingPolicyRef))
	fmt.Fprintf(tw, "policy_version\t%s\n", dashIfEmpty(decision.PolicyVersion))
	fmt.Fprintf(tw, "selection_mode\t%s\n", decision.SelectionMode)
	fmt.Fprintf(tw, "selected_agent\t%s\n", decision.SelectedAgent)
	fmt.Fprintf(tw, "selected_repo_mode\t%s\n", decision.SelectedRepoMode)
	fmt.Fprintf(tw, "route_reason\t%s\n", dashIfEmpty(decision.RouteReason))
	_ = tw.Flush()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
