package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/controlplane"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

type reconcileSummary = controlplane.ReconcileSummary
type reconcileManagedRepoStatus = controlplane.ReconcileManagedRepoStatus
type reconcileReport = controlplane.ReconcileReport

var (
	reconcileOnceReadOnly bool
	reconcileOnceJSON     bool
)

var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Compare desired state in the DB against observed local state",
}

var reconcileOnceCmd = &cobra.Command{
	Use:   "once",
	Short: "Run one reconcile pass against applied desired state",
	Long: `Run one reconcile pass against applied desired state.

Today only read-only observation is supported. agentctl reads ManagedRepo
resources from the local DB, checks whether local clones are present,
observes origin remote reachability/default branch when available, and
verifies whether the configured repo contract file exists.`,
	Args: cobra.NoArgs,
	RunE: runReconcileOnce,
}

func init() {
	rootCmd.AddCommand(reconcileCmd)
	reconcileCmd.AddCommand(reconcileOnceCmd)
	reconcileOnceCmd.Flags().BoolVar(&reconcileOnceReadOnly, "read-only", false, "Observe local state without mutating runtime state")
	reconcileOnceCmd.Flags().BoolVar(&reconcileOnceJSON, "json", false, "Output machine-readable JSON")
}

func runReconcileOnce(cmd *cobra.Command, args []string) error {
	if !reconcileOnceReadOnly {
		return fmt.Errorf("only --read-only is supported today")
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	report, err := buildReconcileReport(db)
	if err != nil {
		return err
	}

	if reconcileOnceJSON {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding json: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "=== Reconcile Once (%s) ===\n", report.Mode)
	fmt.Fprintf(cmd.OutOrStdout(), "ManagedRepos=%d LocalClonesFound=%d RepoContractsFound=%d RemoteURLsResolved=%d RemoteReachable=%d RemoteDefaultBranchesResolved=%d NeedsAttention=%d\n",
		report.Summary.ManagedRepos,
		report.Summary.LocalClonesFound,
		report.Summary.RepoContractsFound,
		report.Summary.RemoteURLsResolved,
		report.Summary.RemoteReachable,
		report.Summary.RemoteDefaultBranchesResolved,
		report.Summary.NeedsAttention,
	)
	if len(report.Repos) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed repos found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPOSITORY\tTIER\tLOCAL\tREMOTE\tDEFAULT_BRANCH\tCONTRACT\tATTENTION\tISSUES")
	for _, repo := range report.Repos {
		tier := repo.Tier
		if tier == "" {
			tier = "-"
		}
		defaultBranch := repo.RemoteDefaultBranch
		if defaultBranch == "" {
			defaultBranch = "-"
		}
		issues := "-"
		if len(repo.Issues) > 0 {
			issues = strings.Join(repo.Issues, "; ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%t\t%s\t%t\t%t\t%s\n",
			repo.Name, repo.Repository, tier, repo.LocalCloneFound, repo.RemoteReachable, defaultBranch, repo.HasRepoContract, repo.NeedsAttention, issues)
	}
	return w.Flush()
}

func buildReconcileReport(db *sql.DB) (*reconcileReport, error) {
	report, err := controlplane.BuildReconcileReport(db)
	if err != nil {
		return nil, err
	}
	return report, nil
}
