package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

type reconcileSummary struct {
	ManagedRepos       int `json:"managedRepos"`
	LocalClonesFound   int `json:"localClonesFound"`
	RepoContractsFound int `json:"repoContractsFound"`
	NeedsAttention     int `json:"needsAttention"`
}

type reconcileManagedRepoStatus struct {
	Name             string   `json:"name"`
	Repository       string   `json:"repository"`
	Tier             string   `json:"tier,omitempty"`
	Role             string   `json:"role,omitempty"`
	LocalPath        string   `json:"localPath,omitempty"`
	LocalCloneFound  bool     `json:"localCloneFound"`
	RepoContractPath string   `json:"repoContractPath,omitempty"`
	HasRepoContract  bool     `json:"hasRepoContract"`
	NeedsAttention   bool     `json:"needsAttention"`
	Issues           []string `json:"issues,omitempty"`
}

type reconcileReport struct {
	Mode    string                       `json:"mode"`
	Summary reconcileSummary             `json:"summary"`
	Repos   []reconcileManagedRepoStatus `json:"repos"`
}

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
resources from the local DB, checks whether local clones are present, and
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
	fmt.Fprintf(cmd.OutOrStdout(), "ManagedRepos=%d LocalClonesFound=%d RepoContractsFound=%d NeedsAttention=%d\n",
		report.Summary.ManagedRepos,
		report.Summary.LocalClonesFound,
		report.Summary.RepoContractsFound,
		report.Summary.NeedsAttention,
	)
	if len(report.Repos) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed repos found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPOSITORY\tTIER\tLOCAL\tCONTRACT\tATTENTION\tISSUES")
	for _, repo := range report.Repos {
		tier := repo.Tier
		if tier == "" {
			tier = "-"
		}
		issues := "-"
		if len(repo.Issues) > 0 {
			issues = strings.Join(repo.Issues, "; ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%t\t%t\t%s\n",
			repo.Name, repo.Repository, tier, repo.LocalCloneFound, repo.HasRepoContract, repo.NeedsAttention, issues)
	}
	return w.Flush()
}

func buildReconcileReport(db *sql.DB) (*reconcileReport, error) {
	managedRepos, err := store.ListManagedRepos(db)
	if err != nil {
		return nil, fmt.Errorf("listing managed repos: %w", err)
	}

	report := &reconcileReport{
		Mode: "read-only",
		Summary: reconcileSummary{
			ManagedRepos: len(managedRepos),
		},
		Repos: make([]reconcileManagedRepoStatus, 0, len(managedRepos)),
	}

	for _, repo := range managedRepos {
		status := observeManagedRepo(repo)
		if status.LocalCloneFound {
			report.Summary.LocalClonesFound++
		}
		if status.HasRepoContract {
			report.Summary.RepoContractsFound++
		}
		if status.NeedsAttention {
			report.Summary.NeedsAttention++
		}
		report.Repos = append(report.Repos, status)
	}

	return report, nil
}

func observeManagedRepo(repo store.ManagedRepo) reconcileManagedRepoStatus {
	desired := buildManagedRepoReport(repo)
	status := reconcileManagedRepoStatus{
		Name:             desired.Name,
		Repository:       desired.Repository,
		Tier:             desired.Tier,
		Role:             desired.Role,
		RepoContractPath: desired.RepoContractPath,
	}

	entry, err := ResolveRepoPath(desired.Repository)
	if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("local clone not found: %v", err))
	} else {
		status.LocalCloneFound = true
		status.LocalPath = entry.FullPath
	}

	contractPath := strings.TrimSpace(desired.RepoContractPath)
	if contractPath == "" {
		status.Issues = append(status.Issues, "repoContractPath is not configured")
		status.NeedsAttention = true
		return status
	}

	if !status.LocalCloneFound {
		status.NeedsAttention = true
		return status
	}

	observedContract := filepath.Join(status.LocalPath, filepath.FromSlash(contractPath))
	info, err := os.Stat(observedContract)
	switch {
	case err == nil && !info.IsDir():
		status.HasRepoContract = true
	case err == nil && info.IsDir():
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract path is a directory: %s", contractPath))
	case os.IsNotExist(err):
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract not found: %s", contractPath))
	default:
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract check failed: %v", err))
	}

	status.NeedsAttention = len(status.Issues) > 0
	return status
}
