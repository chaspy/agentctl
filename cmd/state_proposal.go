package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var stateProposalJSON bool

var stateProposalCmd = &cobra.Command{
	Use:   "proposal",
	Short: "Inspect persisted task proposal snapshots",
}

var stateProposalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persisted task proposal snapshots from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateProposalList,
}

var stateProposalGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one persisted task proposal snapshot from the local DB",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateProposalGet,
}

func init() {
	stateCmd.AddCommand(stateProposalCmd)
	stateProposalCmd.AddCommand(stateProposalListCmd)
	stateProposalCmd.AddCommand(stateProposalGetCmd)
	stateProposalListCmd.Flags().BoolVar(&stateProposalJSON, "json", false, "Output machine-readable JSON")
	stateProposalGetCmd.Flags().BoolVar(&stateProposalJSON, "json", false, "Output machine-readable JSON")
}

func runStateProposalList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	proposals, err := store.ListTaskProposalSnapshots(db)
	if err != nil {
		return fmt.Errorf("listing task proposal snapshots: %w", err)
	}
	if stateProposalJSON {
		return writeProposalJSON(cmd.OutOrStdout(), proposals)
	}
	if len(proposals) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No task proposal snapshots found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSOURCE\tREPO\tCATEGORY\tTASK_TYPE\tRISK\tAPPROVAL\tUPDATED")
	for _, proposal := range proposals {
		updated := proposal.UpdatedAt
		if updated == "" {
			updated = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			proposal.ID,
			proposal.Source,
			proposal.RepoRef,
			proposal.Category,
			proposal.TaskType,
			proposal.Risk,
			proposal.ApprovalStatus,
			updated,
		)
	}
	return w.Flush()
}

func runStateProposalGet(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	proposal, err := store.GetTaskProposalSnapshot(db, args[0])
	if err != nil {
		return fmt.Errorf("getting task proposal snapshot %s: %w", args[0], err)
	}
	if proposal == nil {
		return fmt.Errorf("task proposal snapshot %q not found", args[0])
	}
	if stateProposalJSON {
		return writeProposalJSON(cmd.OutOrStdout(), proposal)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ID:                %s\n", proposal.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Source:            %s\n", emptyFallback(proposal.Source))
	fmt.Fprintf(cmd.OutOrStdout(), "ReportMode:        %s\n", emptyFallback(proposal.ReportMode))
	fmt.Fprintf(cmd.OutOrStdout(), "RepoRef:           %s\n", proposal.RepoRef)
	fmt.Fprintf(cmd.OutOrStdout(), "Repository:        %s\n", proposal.Repository)
	fmt.Fprintf(cmd.OutOrStdout(), "Tier:              %s\n", emptyFallback(proposal.Tier))
	fmt.Fprintf(cmd.OutOrStdout(), "Category:          %s\n", proposal.Category)
	fmt.Fprintf(cmd.OutOrStdout(), "Title:             %s\n", proposal.Title)
	fmt.Fprintf(cmd.OutOrStdout(), "Objective:         %s\n", proposal.Objective)
	fmt.Fprintf(cmd.OutOrStdout(), "TaskType:          %s\n", proposal.TaskType)
	fmt.Fprintf(cmd.OutOrStdout(), "Risk:              %s\n", proposal.Risk)
	fmt.Fprintf(cmd.OutOrStdout(), "ReviewPolicyRef:   %s\n", emptyFallback(proposal.ReviewPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalPolicyRef: %s\n", emptyFallback(proposal.ApprovalPolicyRef))
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalStatus:    %s\n", proposal.ApprovalStatus)
	fmt.Fprintf(cmd.OutOrStdout(), "ApprovalReason:    %s\n", emptyFallback(proposal.ApprovalReason))
	if len(proposal.DesiredOutcome) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "DesiredOutcome:")
		for _, item := range proposal.DesiredOutcome {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	if len(proposal.TriggerIssues) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "TriggerIssues:")
		for _, item := range proposal.TriggerIssues {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:           %s\n", emptyFallback(proposal.UpdatedAt))
	return nil
}

func writeProposalJSON(w io.Writer, value any) error {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
