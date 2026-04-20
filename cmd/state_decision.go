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

var stateDecisionJSON bool
var (
	stateDecisionAttemptDryRun  bool
	stateDecisionAttemptBranch  string
	stateDecisionAttemptName    string
	stateDecisionAttemptMessage string
	stateDecisionAttemptSummary string
)

var stateDecisionCmd = &cobra.Command{
	Use:   "decision",
	Short: "Inspect recorded AgentTask routing decisions",
}

var stateDecisionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AgentTask routing decisions from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateDecisionList,
}

var stateDecisionGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one AgentTask routing decision from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateDecisionGet,
}

var stateDecisionAttemptCmd = &cobra.Command{
	Use:   "attempt <id>",
	Short: "Create and execute one Attempt from a recorded Decision",
	Long: `Builds an AgentTask Attempt from a recorded Decision and executes the spawn path.
Use --dry-run to preview the attempt without writing to SQLite or starting a session.`,
	Args: cobra.ExactArgs(1),
	RunE: runStateDecisionAttempt,
}

func init() {
	stateCmd.AddCommand(stateDecisionCmd)
	stateDecisionCmd.AddCommand(stateDecisionListCmd)
	stateDecisionCmd.AddCommand(stateDecisionGetCmd)
	stateDecisionCmd.AddCommand(stateDecisionAttemptCmd)
	stateDecisionListCmd.Flags().BoolVar(&stateDecisionJSON, "json", false, "Output machine-readable JSON")
	stateDecisionGetCmd.Flags().BoolVar(&stateDecisionJSON, "json", false, "Output machine-readable JSON")
	stateDecisionAttemptCmd.Flags().BoolVar(&stateDecisionAttemptDryRun, "dry-run", false, "Preview the attempt without writing to SQLite or spawning")
	stateDecisionAttemptCmd.Flags().StringVar(&stateDecisionAttemptBranch, "branch", "", "Branch override for the spawned worktree/session")
	stateDecisionAttemptCmd.Flags().StringVar(&stateDecisionAttemptName, "name", "", "Zellij session name override")
	stateDecisionAttemptCmd.Flags().StringVar(&stateDecisionAttemptMessage, "message", "", "Initial instruction override")
	stateDecisionAttemptCmd.Flags().StringVar(&stateDecisionAttemptSummary, "summary", "", "Task summary override")
}

func runStateDecisionList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	decisions, err := store.ListAgentTaskDecisions(db)
	if err != nil {
		return fmt.Errorf("listing decisions: %w", err)
	}
	if stateDecisionJSON {
		return writeDecisionJSON(cmd.OutOrStdout(), decisions)
	}
	if len(decisions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No decisions found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAGENT_TASK\tREPO\tTASK_TYPE\tSELECTED_AGENT\tREPO_MODE\tSTATUS\tCREATED")
	for _, decision := range decisions {
		created := decision.CreatedAt
		if created == "" {
			created = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			decision.ID,
			decision.AgentTaskName,
			decision.RepoRef,
			decision.TaskType,
			decision.SelectedAgent,
			decision.SelectedRepoMode,
			decision.Status,
			created,
		)
	}
	return w.Flush()
}

func runStateDecisionGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid decision ID %q: %w", args[0], err)
	}
	decision, err := store.GetAgentTaskDecision(db, id)
	if err != nil {
		return fmt.Errorf("getting decision %s: %w", args[0], err)
	}
	if decision == nil {
		return fmt.Errorf("decision %q not found", args[0])
	}
	if stateDecisionJSON {
		return writeDecisionJSON(cmd.OutOrStdout(), decision)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:               %d\n", decision.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "AgentTask:        %s\n", decision.AgentTaskName)
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:          %s\n", decision.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:       %s\n", decision.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:         %s\n", decision.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:             %s\n", decision.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "RoutingPolicyRef: %s\n", emptyFallback(decision.RoutingPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "PolicyVersion:    %s\n", emptyFallback(decision.PolicyVersion))
	fmt.Fprintf(cmd.OutOrStdout(), "SelectionMode:    %s\n", emptyFallback(decision.SelectionMode))
	fmt.Fprintf(cmd.OutOrStdout(), "SelectedAgent:    %s\n", decision.SelectedAgent)
	fmt.Fprintf(cmd.OutOrStdout(), "SelectedRepoMode: %s\n", emptyFallback(decision.SelectedRepoMode))
	fmt.Fprintf(cmd.OutOrStdout(), "RepoProfileSource:%s\n", " "+emptyFallback(decision.RepoProfileSource))
	fmt.Fprintf(cmd.OutOrStdout(), "ModeSource:       %s\n", emptyFallback(decision.ModeSource))
	fmt.Fprintf(cmd.OutOrStdout(), "AgentSource:      %s\n", emptyFallback(decision.AgentSource))
	fmt.Fprintf(cmd.OutOrStdout(), "RouteReason:      %s\n", emptyFallback(decision.RouteReason))
	fmt.Fprintf(cmd.OutOrStdout(), "Status:           %s\n", decision.Status)
	if len(decision.EligibleAgents) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "EligibleAgents:")
		for _, item := range decision.EligibleAgents {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	if len(decision.CandidateScores) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "CandidateScores:")
		for _, item := range decision.CandidateScores {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s score=%.2f available=%t reason=%s\n", item.Agent, item.Score, item.Available, item.Reason)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created:          %s\n", emptyFallback(decision.CreatedAt))
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:          %s\n", emptyFallback(decision.UpdatedAt))
	return nil
}

func writeDecisionJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func runStateDecisionAttempt(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid decision ID %q: %w", args[0], err)
	}
	decision, err := store.GetAgentTaskDecision(db, id)
	if err != nil {
		return fmt.Errorf("getting decision %s: %w", args[0], err)
	}
	if decision == nil {
		return fmt.Errorf("decision %q not found", args[0])
	}
	if decision.Status != "recorded" {
		return fmt.Errorf("decision %d is %q, expected recorded", decision.ID, decision.Status)
	}

	task, err := store.GetAgentTask(db, decision.AgentTaskName)
	if err != nil {
		return fmt.Errorf("getting agent task %s: %w", decision.AgentTaskName, err)
	}
	if task == nil {
		return fmt.Errorf("agent task %q not found", decision.AgentTaskName)
	}
	if task.Status != "routed" {
		return fmt.Errorf("agent task %q is %q, expected routed", task.Name, task.Status)
	}

	plan, err := buildAgentTaskAttemptPlan(task, decision, stateDecisionAttemptBranch, stateDecisionAttemptName, stateDecisionAttemptMessage, stateDecisionAttemptSummary)
	if err != nil {
		return fmt.Errorf("building attempt plan: %w", err)
	}
	if stateDecisionAttemptDryRun {
		writeAgentTaskAttemptPlan(cmd.OutOrStdout(), plan.Attempt, true)
		fmt.Fprintln(cmd.OutOrStdout(), "No database changes were made.")
		return nil
	}

	if err := store.CreateAgentTaskAttempt(db, plan.Attempt); err != nil {
		return fmt.Errorf("creating attempt: %w", err)
	}

	result, execErr := runAgentTaskAttemptSpawn(plan)
	if execErr != nil {
		plan.Attempt.Status = "failed"
		plan.Attempt.FailureReason = execErr.Error()
		if err := store.UpdateAgentTaskAttempt(db, plan.Attempt); err != nil {
			return fmt.Errorf("updating failed attempt #%d: %w", plan.Attempt.ID, err)
		}
		writeAgentTaskAttemptPlan(cmd.OutOrStdout(), plan.Attempt, false)
		return fmt.Errorf("executing attempt #%d: %w", plan.Attempt.ID, execErr)
	}

	plan.Attempt.Status = "spawned"
	plan.Attempt.ManagedSessionID = result.SessionDBID
	plan.Attempt.SessionName = result.SessionName
	plan.Attempt.WorkDir = result.WorkDir
	plan.Attempt.Branch = result.GitBranch
	plan.Attempt.LaunchCommand = result.LaunchCommand
	plan.Attempt.FailureReason = ""
	if err := store.UpdateAgentTaskAttempt(db, plan.Attempt); err != nil {
		return fmt.Errorf("finalizing attempt #%d: %w", plan.Attempt.ID, err)
	}
	if err := store.UpdateAgentTaskDecisionStatus(db, decision.ID, "applied"); err != nil {
		return fmt.Errorf("marking decision applied: %w", err)
	}
	if err := store.UpdateAgentTaskStatus(db, task.Name, "spawned"); err != nil {
		return fmt.Errorf("marking agent task spawned: %w", err)
	}
	if err := store.LogAction(db, &store.Action{
		SessionID:   plan.Attempt.ManagedSessionID,
		ActionType:  "attempt",
		Content:     fmt.Sprintf("Spawned attempt #%d from decision #%d for %s", plan.Attempt.ID, decision.ID, task.Name),
		Result:      plan.Attempt.SessionName,
		RouteReason: decision.RouteReason,
	}); err != nil {
		return fmt.Errorf("logging attempt action: %w", err)
	}

	writeAgentTaskAttemptPlan(cmd.OutOrStdout(), plan.Attempt, false)
	fmt.Fprintf(cmd.OutOrStdout(), "Executed attempt #%d from Decision %d.\n", plan.Attempt.ID, decision.ID)
	return nil
}

func writeAgentTaskAttemptPlan(w io.Writer, attempt *store.AgentTaskAttempt, dryRun bool) {
	title := "AgentTask attempt"
	if dryRun {
		title += " (dry-run)"
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "decision_id\t%d\n", attempt.DecisionID)
	fmt.Fprintf(tw, "agent_task\t%s\n", attempt.AgentTaskName)
	fmt.Fprintf(tw, "repo\t%s\n", attempt.RepoRef)
	fmt.Fprintf(tw, "task_type\t%s\n", attempt.TaskType)
	fmt.Fprintf(tw, "risk\t%s\n", attempt.Risk)
	fmt.Fprintf(tw, "agent\t%s\n", attempt.Agent)
	fmt.Fprintf(tw, "repo_mode\t%s\n", dashIfEmpty(attempt.RepoMode))
	fmt.Fprintf(tw, "branch\t%s\n", dashIfEmpty(attempt.Branch))
	fmt.Fprintf(tw, "session_name\t%s\n", dashIfEmpty(attempt.SessionName))
	fmt.Fprintf(tw, "launch_command\t%s\n", dashIfEmpty(attempt.LaunchCommand))
	fmt.Fprintf(tw, "status\t%s\n", dashIfEmpty(attempt.Status))
	if attempt.ManagedSessionID != "" {
		fmt.Fprintf(tw, "managed_session_id\t%s\n", attempt.ManagedSessionID)
	}
	if attempt.WorkDir != "" {
		fmt.Fprintf(tw, "work_dir\t%s\n", attempt.WorkDir)
	}
	if attempt.FailureReason != "" {
		fmt.Fprintf(tw, "failure_reason\t%s\n", attempt.FailureReason)
	}
	_ = tw.Flush()
}
