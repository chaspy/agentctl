package cmd

import (
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

func init() {
	stateCmd.AddCommand(stateAgentTaskCmd)
	stateAgentTaskCmd.AddCommand(stateAgentTaskListCmd)
	stateAgentTaskCmd.AddCommand(stateAgentTaskGetCmd)
	stateAgentTaskListCmd.Flags().BoolVar(&stateAgentTaskJSON, "json", false, "Output machine-readable JSON")
	stateAgentTaskGetCmd.Flags().BoolVar(&stateAgentTaskJSON, "json", false, "Output machine-readable JSON")
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

func writeAgentTaskJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
