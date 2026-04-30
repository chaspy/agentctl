package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	stateAdoptionJSON bool
)

var stateAdoptionCmd = &cobra.Command{
	Use:   "adoption",
	Short: "Inspect queued live-session adoptions",
}

var stateAdoptionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List live-session adoptions from the local DB",
	Args:  cobra.NoArgs,
	RunE:  runStateAdoptionList,
}

var stateAdoptionCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a queued live-session adoption",
	Args:  cobra.ExactArgs(1),
	RunE:  runStateAdoptionCancel,
}

func init() {
	stateCmd.AddCommand(stateAdoptionCmd)
	stateAdoptionCmd.AddCommand(stateAdoptionListCmd)
	stateAdoptionCmd.AddCommand(stateAdoptionCancelCmd)
	stateAdoptionListCmd.Flags().BoolVar(&stateAdoptionJSON, "json", false, "Output machine-readable JSON")
}

func runStateAdoptionList(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	adoptions, err := store.ListSessionAdoptions(db)
	if err != nil {
		return fmt.Errorf("listing session adoptions: %w", err)
	}
	if stateAdoptionJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(adoptions)
	}
	if len(adoptions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No session adoptions found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAGENT\tMUX\tSESSION\tREPO\tBRANCH\tPERMISSION\tSTATUS\tNOTE\tUPDATED")
	for _, adoption := range adoptions {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%d (%s)\t%s\t%s\t%s\n",
			adoption.ID,
			adoption.Agent,
			adoption.Mux,
			adoption.ZellijSession,
			adoption.Repository,
			adoption.GitBranch,
			adoption.TargetPermissionLevel,
			store.PermissionLabel(adoption.TargetPermissionLevel),
			adoption.Status,
			emptyFallback(adoption.Note),
			adoption.UpdatedAt.Format("2006-01-02 15:04:05"),
		)
	}
	return w.Flush()
}

func runStateAdoptionCancel(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid adoption id %q: %w", args[0], err)
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	adoption, err := store.GetSessionAdoption(db, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("session adoption %d not found", id)
		}
		return fmt.Errorf("getting session adoption %d: %w", id, err)
	}
	if adoption.Status != store.AdoptionStatusQueued {
		return fmt.Errorf("session adoption %d is %q, only queued adoptions can be cancelled", id, adoption.Status)
	}
	if err := store.UpdateSessionAdoptionStatus(db, id, store.AdoptionStatusCancelled); err != nil {
		return fmt.Errorf("cancelling session adoption %d: %w", id, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Cancelled session adoption #%d (%s).\n", adoption.ID, adoption.ZellijSession)
	return nil
}
