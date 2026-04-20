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
	reconcileOnceReadOnly     bool
	reconcileOnceJSON         bool
	reconcilePersistProposals bool
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
verifies whether the configured repo contract file exists.

Use --persist-proposals to write the generated task proposal snapshot into
the local DB for future client / approval flow experiments.`,
	Args: cobra.NoArgs,
	RunE: runReconcileOnce,
}

func init() {
	rootCmd.AddCommand(reconcileCmd)
	reconcileCmd.AddCommand(reconcileOnceCmd)
	reconcileOnceCmd.Flags().BoolVar(&reconcileOnceReadOnly, "read-only", false, "Observe local state without mutating runtime state")
	reconcileOnceCmd.Flags().BoolVar(&reconcileOnceJSON, "json", false, "Output machine-readable JSON")
	reconcileOnceCmd.Flags().BoolVar(&reconcilePersistProposals, "persist-proposals", false, "Persist generated task proposal snapshots into the local DB")
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
	if reconcilePersistProposals {
		if err := persistTaskProposalSnapshots(db, report); err != nil {
			return fmt.Errorf("persist task proposals: %w", err)
		}
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
	fmt.Fprintf(cmd.OutOrStdout(), "ManagedRepos=%d LocalClonesFound=%d RepoContractsFound=%d RemoteURLsResolved=%d RemoteReachable=%d RemoteDefaultBranchesResolved=%d TaskProposals=%d ApprovalRequiredProposals=%d ApprovalUnknownProposals=%d NeedsAttention=%d\n",
		report.Summary.ManagedRepos,
		report.Summary.LocalClonesFound,
		report.Summary.RepoContractsFound,
		report.Summary.RemoteURLsResolved,
		report.Summary.RemoteReachable,
		report.Summary.RemoteDefaultBranchesResolved,
		report.Summary.TaskProposals,
		report.Summary.ApprovalRequiredProposals,
		report.Summary.ApprovalUnknownProposals,
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
	if err := w.Flush(); err != nil {
		return err
	}

	if len(report.Proposals) == 0 {
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "=== Task Proposals (%d) ===\n", len(report.Proposals))
	w = tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tREPO\tCATEGORY\tTASK_TYPE\tRISK\tAPPROVAL\tTITLE")
	for _, proposal := range report.Proposals {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			proposal.ID,
			proposal.RepoRef,
			proposal.Category,
			proposal.TaskType,
			proposal.Risk,
			proposal.Approval.Status,
			proposal.Title,
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if reconcilePersistProposals {
		fmt.Fprintf(cmd.OutOrStdout(), "\nPersisted %d task proposal snapshot(s).\n", len(report.Proposals))
	}
	return nil
}

func buildReconcileReport(db *sql.DB) (*reconcileReport, error) {
	report, err := controlplane.BuildReconcileReport(db)
	if err != nil {
		return nil, err
	}
	return report, nil
}

func persistTaskProposalSnapshots(db *sql.DB, report *reconcileReport) error {
	snapshots := make([]store.TaskProposalSnapshot, 0, len(report.Proposals))
	for _, proposal := range report.Proposals {
		rawProposalJSON, err := json.Marshal(proposal)
		if err != nil {
			return fmt.Errorf("marshal proposal %s: %w", proposal.ID, err)
		}
		snapshots = append(snapshots, store.TaskProposalSnapshot{
			ID:                proposal.ID,
			Source:            "reconcile",
			ReportMode:        report.Mode,
			RepoRef:           proposal.RepoRef,
			Repository:        proposal.Repository,
			Tier:              proposal.Tier,
			Category:          proposal.Category,
			Title:             proposal.Title,
			Objective:         proposal.Objective,
			TaskType:          proposal.TaskType,
			Risk:              proposal.Risk,
			ReviewPolicyRef:   proposal.ReviewPolicyRef,
			ApprovalPolicyRef: proposal.ApprovalPolicyRef,
			ApprovalStatus:    proposal.Approval.Status,
			ApprovalReason:    proposal.Approval.Reason,
			DesiredOutcome:    append([]string(nil), proposal.DesiredOutcome...),
			TriggerIssues:     append([]string(nil), proposal.TriggerIssues...),
			RawProposalJSON:   string(rawProposalJSON),
		})
	}
	return store.ReplaceTaskProposalSnapshots(db, "reconcile", snapshots)
}
