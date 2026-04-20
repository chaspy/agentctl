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

func init() {
	stateCmd.AddCommand(stateDecisionCmd)
	stateDecisionCmd.AddCommand(stateDecisionListCmd)
	stateDecisionCmd.AddCommand(stateDecisionGetCmd)
	stateDecisionListCmd.Flags().BoolVar(&stateDecisionJSON, "json", false, "Output machine-readable JSON")
	stateDecisionGetCmd.Flags().BoolVar(&stateDecisionJSON, "json", false, "Output machine-readable JSON")
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
